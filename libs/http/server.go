package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type (
	Handler func(*Context)
	Server  struct {
		*util.Module
		sync.Mutex
		prefix      string
		methods     []string
		node        *Node
		middlewares []Handler
		config      *serverConfig
		httpServer  *http.Server
		closed      atomic.Bool
		ctx         *Context
		ctxQueue    chan *Context
		context     context.Context
		cancel      context.CancelFunc
		wg          sync.WaitGroup
		once        sync.Once
	}
	serverConfig struct {
		CertFile          string         // 证书文件
		KeyFile           string         // 私钥文件
		Addr              string         // 监听地址
		errorTemplate     string         // 错误模板
		Workers           int            // 工作协程
		QueueSize         int            // 请求队列
		ReadTimeout       time.Duration  // 读取超时
		WriteTimeout      time.Duration  // 写入超时
		IdleTimeout       time.Duration  // 空闲超时
		ProcessingTimeout time.Duration  // 处理超时
		ShutdownTimeout   time.Duration  // 关闭超时
		onRequest         *lua.LFunction // 查询记录
		onError           *lua.LFunction // 服务错误
		onSuccess         *lua.LFunction // 服务成功
		onShutdown        *lua.LFunction // 服务关闭
	}
)

func extendMethod(s *Server) util.Methods {
	return util.Methods{
		"use":     s.Use,
		"any":     s.handle(`*`),
		"connect": s.handle(http.MethodConnect),
		"delete":  s.handle(http.MethodDelete),
		"get":     s.handle(http.MethodGet),
		"head":    s.handle(http.MethodHead),
		"options": s.handle(http.MethodOptions),
		"patch":   s.handle(http.MethodPatch),
		"post":    s.handle(http.MethodPost),
		"put":     s.handle(http.MethodPut),
		"trace":   s.handle(http.MethodTrace),
	}
}

func newServer(L *lua.LState) int {
	cfg := &serverConfig{
		Addr:              ":3000",
		Workers:           100,
		QueueSize:         1000,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ProcessingTimeout: 30 * time.Second,
		ShutdownTimeout:   60 * time.Second,
	}

	if L.GetTop() >= 1 {
		opts := L.CheckTable(1)
		cfg = getServerConfig(L, opts, cfg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		Module:   util.NewModule(L),
		node:     newNode(),
		config:   cfg,
		ctxQueue: make(chan *Context, cfg.QueueSize),
		context:  ctx,
		cancel:   cancel,
	}

	methods := extendMethod(server)
	server.SetMethods(methods, util.Methods{
		"listen":      server.Listen,
		"shutdown":    server.Shutdown,
		"group":       server.Group,
		"stripPrefix": server.StripPrefix,
	})

	return server.Self()
}

func (s *Server) Group(L *lua.LState) int {
	pattern := L.CheckString(1)
	s.Lock()
	defer s.Unlock()
	group := &Server{
		Module:      util.NewModule(L),
		prefix:      s.pathJoin(pattern),
		node:        s.node,
		middlewares: append([]Handler{}, s.middlewares...),
		config:      s.config,
	}
	methods := extendMethod(group)
	return group.SetMethods(methods).Self()
}

func (s *Server) Use(L *lua.LState) int {
	n := L.GetTop()
	s.Lock()
	defer s.Unlock()
	for i := 1; i <= n; i++ {
		handler := s.luaHttpHandler(L.CheckFunction(i), true)
		s.middlewares = append(s.middlewares, handler)
	}
	return s.Self()
}

func (s *Server) handle(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		path := s.pathJoin(L.CheckString(1))
		handler := s.luaHttpHandler(L.CheckFunction(2), false)

		s.Lock()
		s.methods = append(s.methods, method)
		methods := make([]string, len(s.methods))
		copy(methods, s.methods)
		s.Unlock()

		for _, m := range methods {
			mUpper := strings.ToUpper(m)
			if err := s.node.add(mUpper, path, handler, s.middlewares); err != nil {
				L.RaiseError(err.Error())
			}
		}

		s.Lock()
		s.methods = nil
		s.Unlock()
		return s.Self()
	}
}

func (s *Server) StripPrefix(L *lua.LState) int {
	prefix := L.CheckString(1)
	fn := L.CheckFunction(2)

	strippedLuaFn := L.NewClosure(func(L *lua.LState) int {
		r := s.ctx.r.Request
		path := r.URL.Path
		if strings.HasPrefix(path, prefix) {
			path = strings.TrimPrefix(path, prefix)
			if path == "" {
				path = "/"
			}
			r.SetPathValue("prefix", prefix)
		}
		r.SetPathValue("path", path)
		s.luaHttpHandler(fn, false)(s.ctx)
		return 0
	})

	L.Push(strippedLuaFn)
	return 1
}

func (s *Server) luaHttpHandler(handler *lua.LFunction, hasNext bool) Handler {
	return func(ctx *Context) {
		s.Vm.SetTop(0) // 清空栈
		s.Vm.Push(handler)
		s.Vm.Push(ctx.w.getMethods(s.Vm))
		s.Vm.Push(ctx.r.getMethods(s.Vm))

		var err error
		if hasNext {
			s.Vm.Push(s.Vm.NewClosure(ctx.Next))
			err = s.Vm.PCall(3, 0, nil)
		} else {
			err = s.Vm.PCall(2, 0, nil)
		}

		if err != nil {
			s.logger("request", err.Error(), http.StatusInternalServerError)
			s.respondError(http.StatusInternalServerError, err)
		}
	}
}

func (s *Server) Listen(L *lua.LState) int {
	s.config.Addr = L.CheckString(1)
	if err := s.start(); err != nil {
		L.Push(lua.LString(err.Error()))
		s.logger("error", "%v", err.Error())
		return 1
	}
	return 0
}

func (s *Server) Shutdown(L *lua.LState) int {
	s.shutdown()
	return 0
}

func (s *Server) logger(t string, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	var callback *lua.LFunction

	switch t {
	case `error`:
		callback = s.config.onError
	case `success`:
		callback = s.config.onSuccess
	case `shutdown`:
		callback = s.config.onShutdown
	case `request`:
		callback = s.config.onRequest
	}
	if callback != nil {
		if err := util.CallLua(s.Vm, callback, lua.LString(msg)); err != nil {
			s.Vm.RaiseError(err.Error())
		}
	} else {
		log.Println(msg)
	}
}

func (s *Server) respondError(statusCode int, err error) {
	s.ctx.w.error(statusCode, err)
}

func (s *Server) start() error {
	var startErr error
	s.once.Do(func() {
		listener, err := s.getListener(s.config.Addr)
		if err != nil {
			startErr = err
			return
		}
		s.httpServer = &http.Server{
			Addr:         s.config.Addr,
			Handler:      s,
			ReadTimeout:  s.config.ReadTimeout,
			WriteTimeout: s.config.WriteTimeout,
			IdleTimeout:  s.config.IdleTimeout,
		}

		s.logger("success", "Server starting on %s", s.config.Addr)

		s.startWorkers()
		go s.handleShutdown()
		go s.runServer(listener)
		s.WaitForInterrupt()
	})
	return startErr
}

func (s *Server) shutdown() {
	s.cancel()
	s.logger("shutdown", "server gracefully stopped")
}

func (s *Server) runServer(listener net.Listener) {

	if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger("error", "Server error: %v", err)
		s.cancel()
	}
}

// 等待中断信号
func (s *Server) WaitForInterrupt() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case sig := <-sigChan:
		s.logger("shutdown", "Received signal: %v", sig)
	case <-s.context.Done():
	}
	s.shutdown()
}

// 优雅关闭
func (s *Server) handleShutdown() {
	<-s.context.Done()
	s.initiateShutdown()
}

func (s *Server) initiateShutdown() {
	s.closed.Store(true)
	close(s.ctxQueue)

	timectx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(timectx); err != nil {
		s.logger("error", "Shutdown error: %v", err)
	}
	s.wg.Wait()
}

func (s *Server) startWorkers() {
	for i := 0; i < s.config.Workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
}

func (s *Server) worker() {
	defer s.wg.Done()
	for ctx := range s.ctxQueue {
		s.processRequest(ctx)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.ctx = newContext(w, r)
	s.ctx.w.ErrorTemplate = s.config.errorTemplate

	if s.closed.Load() {
		s.respondError(http.StatusServiceUnavailable, errors.New("service Unavailable"))
		s.ctx.release()
		return
	}

	select {
	case s.ctxQueue <- s.ctx:
		<-s.ctx.Done
	default:
		s.respondError(http.StatusServiceUnavailable, errors.New("service Temporarily Unavailable"))
		s.ctx.release()
	}
}

func (s *Server) requestLog(ctx *Context) {
	// size := util.FormatBytes(int64(ctx.w.Size))
	s.logger("request", "%s %d %s %d", ctx.r.Request.Method, ctx.w.StatusCode, ctx.r.Request.URL.Path, ctx.w.Size)
}

// 请求处理
func (s *Server) processRequest(ctx *Context) {
	defer func() {
		ctx.release()
	}()

	defer func() {
		if err := recover(); err != nil {
			s.logger("error", "Panic recovered: %v", err)
			s.respondError(http.StatusInternalServerError, errors.New("internal server error"))
		}
	}()

	timeoutctx, cancel := context.WithTimeout(context.Background(), s.config.ProcessingTimeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.serve(ctx)
		close(done)
	}()

	select {
	case <-done:
		s.requestLog(ctx)
	case <-timeoutctx.Done():
		ctx.w.TimedOut = true
		s.logger("request", "request timeout")
		s.respondError(http.StatusGatewayTimeout, errors.New("request timeout"))
	}
}

func (s *Server) serve(ctx *Context) {
	node := s.node.find(ctx.r.Request.Method, ctx.r.Request.URL.Path)
	if node == nil {
		s.respondError(http.StatusNotFound, errors.New("page not found"))
		return
	}
	if node.host != "" && node.host != ctx.r.Request.Host {
		s.respondError(http.StatusForbidden, fmt.Errorf("host not allowed: requested '%s', allowed '%s'", ctx.r.Host, node.host))
		return
	}

	ctx.w.Params = node.params
	ctx.r.Params = node.params

	handler := node.handler
	if handler == nil {
		s.respondError(http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if len(node.middlewares) > 0 {
		handler = s.withMiddleware(node.middlewares, handler)
	}
	handler(ctx)
}

// Apply Middleware
func (s *Server) withMiddleware(middlewares []Handler, handler Handler) Handler {
	count := len(middlewares)
	middlewares = append(middlewares, handler)
	for i := count; i >= 0; i-- {
		currentIndex := i
		next := handler
		handler = func(ctx *Context) {
			if currentIndex < count {
				ctx.next = func() { next(ctx) }
			} else {
				ctx.next = nil
			}
			middlewares[currentIndex](ctx)
		}
	}
	return handler
}

func (s *Server) getListener(addr string) (net.Listener, error) {
	if s.config.CertFile == "" || s.config.KeyFile == "" {
		return net.Listen("tcp", addr)
	}

	cert, err := tls.LoadX509KeyPair(s.config.CertFile, s.config.KeyFile)
	if err != nil {
		return nil, err
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	return tls.Listen("tcp", addr, config)
}

func (s *Server) pathJoin(pattern string) string {
	if pattern == "" {
		return s.prefix
	}
	finalPath := path.Join(s.prefix, pattern)
	if strings.HasSuffix(pattern, `/`) && !strings.HasSuffix(finalPath, `/`) {
		return finalPath + `/`
	}
	return finalPath
}

func getServerConfig(L *lua.LState, opts *lua.LTable, cfg *serverConfig) *serverConfig {
	opts.ForEach(func(k lua.LValue, v lua.LValue) {
		key := k.String()
		switch key {

		case "certFile":
			if val, ok := v.(lua.LString); ok {
				cfg.CertFile = val.String()
			} else {
				L.ArgError(1, "certFile must be string")
			}

		case "keyFile":
			if val, ok := v.(lua.LString); ok {
				cfg.KeyFile = val.String()
			} else {
				L.ArgError(1, "keyFile must be string")
			}

		case "readTimeout":
			if val, ok := v.(lua.LNumber); ok {
				cfg.ReadTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(1, "readTimeout must be number(time second)")
			}

		case "writeTimeout":
			if val, ok := v.(lua.LNumber); ok {
				cfg.WriteTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(1, "writeTimeout must be number(time second)")
			}

		case "idleTimeout":
			if val, ok := v.(lua.LNumber); ok {
				cfg.IdleTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(1, "idleTimeout must be number(time second)")
			}

		case "processingTimeout":
			if val, ok := v.(lua.LNumber); ok {
				cfg.ProcessingTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(1, "processingTimeout must be number(time second)")
			}

		case "queueSize":
			if val, ok := v.(lua.LNumber); ok {
				cfg.QueueSize = int(val)
			} else {
				L.ArgError(1, "queueSize must be number")
			}

		case "workers":
			if val, ok := v.(lua.LNumber); ok {
				cfg.Workers = int(val)
			} else {
				L.ArgError(1, "workers must be number")
			}

		case "onRequest":
			if val, ok := v.(*lua.LFunction); ok {
				cfg.onRequest = val
			} else {
				L.ArgError(1, "onRequest must be function")
			}
		case "onError":
			if val, ok := v.(*lua.LFunction); ok {
				cfg.onError = val
			} else {
				L.ArgError(1, "onError must be function")
			}

		case "onSuccess":
			if val, ok := v.(*lua.LFunction); ok {
				cfg.onSuccess = val
			} else {
				L.ArgError(1, "onSuccess must be function")
			}

		case "onShutdown":
			if val, ok := v.(*lua.LFunction); ok {
				cfg.onShutdown = val
			} else {
				L.ArgError(1, "onShutdown must be function")
			}

		case "errorTemplate":
			if val, ok := v.(lua.LString); ok {
				cfg.errorTemplate = val.String()
			} else {
				L.ArgError(1, "errorTemplate must be string")
			}

		}

	})
	return cfg
}
