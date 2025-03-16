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
	Handler    func(*requestCtx)
	requestCtx struct {
		w    *ResponseWriter
		r    *Request
		done chan struct{}
	}
	Server struct {
		*util.Module
		sync.Mutex
		config      *serverConfig
		prefix      string
		methods     []string
		node        *Node
		middlewares []Handler
		httpServer  *http.Server
		closed      atomic.Bool
		wg          sync.WaitGroup
		queue       chan *requestCtx
		ctx         context.Context
		cancel      context.CancelFunc
		once        sync.Once
		requestPool sync.Pool
	}

	serverConfig struct {
		CertFile          string
		KeyFile           string
		Addr              string         // 监听地址
		ReadTimeout       time.Duration  // 读取超时
		WriteTimeout      time.Duration  // 写入超时
		IdleTimeout       time.Duration  // 空闲超时
		QueueSize         int            // 请求队列容量
		Workers           int            // 工作协程数
		ProcessingTimeout time.Duration  // 处理超时
		ShutdownTimeout   time.Duration  // 优雅关闭超时
		Logger            *lua.LFunction // 日志记录器
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

func newServer(L *lua.LState) *lua.LTable {
	config := &serverConfig{
		Addr:              ":8080",
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		QueueSize:         1000,
		Workers:           50,
		ProcessingTimeout: 15 * time.Second,
		ShutdownTimeout:   30 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		Module:      util.NewModule(L),
		node:        newNode(),
		config:      config,
		queue:       make(chan *requestCtx, config.QueueSize),
		ctx:         ctx,
		cancel:      cancel,
		requestPool: sync.Pool{New: func() interface{} { return &requestCtx{} }},
	}

	serverMethods := util.Methods{
		"listen":   server.Listen,
		"shutdown": server.Shutdown,
		"group":    server.Group,
		"config":   server.Config,
	}
	server.SetMethods(extendMethod(server), serverMethods)

	return server.Method
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
		handler := s.makeLuaHandler(L.CheckFunction(i))
		s.middlewares = append(s.middlewares, handler)
	}
	return s.Self()
}

func (s *Server) handle(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		path := s.pathJoin(L.CheckString(1))
		handler := s.makeLuaHandler(L.CheckFunction(2))

		s.Lock()
		s.methods = append(s.methods, method)
		methods := make([]string, len(s.methods))
		copy(methods, s.methods)
		s.Unlock()

		for _, m := range methods {
			mUpper := strings.ToUpper(m)
			s.node.add(mUpper, path, handler, s.middlewares)
		}

		s.Lock()
		s.methods = nil
		s.Unlock()
		return s.Self()
	}
}

func (s *Server) makeLuaHandler(handler *lua.LFunction) Handler {
	return func(ctx *requestCtx) {
		s.Vm.SetTop(0) // 清空栈
		s.Vm.Push(handler)
		s.Vm.Push(ctx.w.getMethods(s.Vm))
		s.Vm.Push(ctx.r.getMethods(s.Vm))
		if err := s.Vm.PCall(2, 0, nil); err != nil {
			s.respondError(ctx, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (s *Server) Listen(L *lua.LState) int {
	s.config.Addr = L.CheckString(1)
	if err := s.start(); err != nil {
		L.Push(lua.LString(err.Error()))
		return 1
	}
	return 0
}

func (s *Server) Shutdown(L *lua.LState) int {
	s.shutdown()
	return 0
}

func (s *Server) logger(format string, v ...interface{}) {
	if s.config.Logger != nil {
		msg := fmt.Sprintf(format, v...)
		if err := util.CallLua(s.Vm, s.config.Logger, msg); err != nil {
			s.Vm.RaiseError(err.Error())
		}
	} else {
		log.Printf(format, v...)
	}
}

func (s *Server) respondError(ctx *requestCtx, message string, status int) {
	http.Error(ctx.w.ResponseWriter, message, status)
}

func (s *Server) requestLog(ctx *requestCtx) {
	if !ctx.w.hijacked {
		log.Printf("%s %s %d %d", ctx.r.Request.Method, ctx.r.Request.URL.Path, ctx.w.status, ctx.w.size)
	}
}

func (s *Server) releaseRequestCtx(ctx *requestCtx) {
	close(ctx.done)
	ctx.w = nil
	ctx.r = nil
	s.requestPool.Put(ctx)
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
		s.logger("Server starting on %s", s.config.Addr)

		s.startWorkers()
		go s.handleShutdown()
		go s.runServer(listener)
		s.WaitForInterrupt()
	})
	return startErr
}

func (s *Server) shutdown() {
	s.cancel()
	s.logger("server gracefully stopped")
}

func (s *Server) runServer(listener net.Listener) {
	if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger("Server error: %v", err)
		s.cancel()
	}
}

// 等待中断信号
func (s *Server) WaitForInterrupt() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case sig := <-sigChan:
		s.logger("Received signal: %v\n", sig)
	case <-s.ctx.Done():
	}
	s.shutdown()
}

// 优雅关闭
func (s *Server) handleShutdown() {
	<-s.ctx.Done()
	s.initiateShutdown()
}

func (s *Server) initiateShutdown() {
	s.closed.Store(true)
	close(s.queue)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger("Shutdown error: %v", err)
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
	for ctx := range s.queue {
		s.processRequest(ctx)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := s.requestPool.Get().(*requestCtx)
	ctx.done = make(chan struct{})
	ctx.w = NewResponse(w, r)
	ctx.r = NewRequest(w, r)

	if s.closed.Load() {
		s.respondError(ctx, "503 service Unavailable", http.StatusServiceUnavailable)
		s.releaseRequestCtx(ctx)
		return
	}

	select {
	case s.queue <- ctx:
		<-ctx.done
	default:
		s.respondError(ctx, "503 service Temporarily Unavailable", http.StatusServiceUnavailable)
		s.releaseRequestCtx(ctx)
	}
}

// 请求处理
func (s *Server) processRequest(ctx *requestCtx) {
	defer func() {
		s.releaseRequestCtx(ctx)
	}()

	defer func() {
		if err := recover(); err != nil {
			s.logger("panic recovered: %v", err)
			s.respondError(ctx, "internal server error", http.StatusInternalServerError)
		}
	}()

	timeoutCtx, cancel := context.WithTimeout(context.Background(), s.config.ProcessingTimeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.serve(ctx)
		close(done)
	}()

	select {
	case <-done:
		s.requestLog(ctx)
	case <-timeoutCtx.Done():
		ctx.w.timedOut = true
		s.respondError(ctx, "Request timeout", http.StatusGatewayTimeout)
	}
}

func (s *Server) serve(ctx *requestCtx) {
	node := s.node.find(ctx.r.Request.Method, ctx.r.Request.URL.Path)
	if node != nil {
		r := context.WithValue(ctx.r.Request.Context(), paramCtxKey, node.params)
		ctx.r.Request = ctx.r.Request.WithContext(r)
	}
	if node == nil {
		s.respondError(ctx, "404 page not found", http.StatusNotFound)
		return
	}
	handler := node.handler
	if handler == nil {
		s.respondError(ctx, "405 method not allowed", http.StatusMethodNotAllowed)
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
		handler = func(c *requestCtx) {
			if currentIndex < count {
				c.r.Next = func() { next(c) }
			} else {
				c.r.Next = nil
			}
			middlewares[currentIndex](c)
		}
	}
	return handler
}

func (s *Server) getListener(addr string) (net.Listener, error) {
	var listener net.Listener
	var err error

	if s.config.CertFile == "" || s.config.KeyFile == "" {
		listener, err = net.Listen("tcp", addr)
	} else {
		if cert, e := tls.LoadX509KeyPair(s.config.CertFile, s.config.KeyFile); e == nil {
			listener, err = tls.Listen("tcp", addr, &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS12,
			})
		}
	}
	return listener, err
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

func (s *Server) Config(L *lua.LState) int {
	L.CheckTable(1).ForEach(func(k lua.LValue, v lua.LValue) {

		if k.String() == `certFile` {
			if val, ok := v.(lua.LString); ok {
				s.config.CertFile = val.String()
			} else {
				L.ArgError(1, "certFile must be string")
			}
		}
		if k.String() == `keyFile` {
			if val, ok := v.(lua.LString); ok {
				s.config.KeyFile = val.String()
			} else {
				L.ArgError(1, "keyFile must be number")
			}
		}
		if k.String() == `readTimeout` {
			if val, ok := v.(lua.LNumber); ok {
				s.config.ReadTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(1, "readTimeout must be number(time second)")
			}
		}
		if k.String() == `writeTimeout` {
			if val, ok := v.(lua.LNumber); ok {
				s.config.WriteTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(1, "writeTimeout must be number(time second)")
			}
		}
		if k.String() == `idleTimeout` {
			if val, ok := v.(lua.LNumber); ok {
				s.config.IdleTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(1, "idleTimeout must be number(time second)")
			}
		}
		if k.String() == `processingTimeout` {
			if val, ok := v.(lua.LNumber); ok {
				s.config.ProcessingTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(1, "processingTimeout must be number(time second)")
			}
		}
		if k.String() == `shutdownTimeout` {
			if val, ok := v.(lua.LNumber); ok {
				s.config.ShutdownTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(1, "shutdownTimeout must be number(time second)")
			}
		}
		if k.String() == `queueSize` {
			if val, ok := v.(lua.LNumber); ok {
				s.config.QueueSize = int(val)
			} else {
				L.ArgError(1, "queueSize must be number")
			}
		}
		if k.String() == `workers` {
			if val, ok := v.(lua.LNumber); ok {
				s.config.Workers = int(val)
			} else {
				L.ArgError(1, "workers must be number")
			}
		}
		if k.String() == `logger` {
			if val, ok := v.(*lua.LFunction); ok {
				s.config.Logger = val
			} else {
				L.ArgError(1, "onError must be function")
			}
		}
	})
	return s.Self()
}
