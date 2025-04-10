package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
	"golang.org/x/sync/semaphore"
)

type (
	Handler *lua.LFunction
	Server  struct {
		*util.Module
		mu          sync.Mutex
		prefix      string
		node        *Node
		middlewares []Handler
		config      *ServerConfig
		httpServer  *http.Server
		semaphore   *semaphore.Weighted
		signalChan  chan os.Signal
		signalOnce  sync.Once
	}
	ServerConfig struct {
		logLevel          string         // 日志等级
		certFile          string         // 证书文件
		keyFile           string         // 私钥文件
		addr              string         // 监听地址
		errorTemplate     string         // 错误模板
		workers           int64          // 最大并发
		readTimeout       time.Duration  // 读取超时
		writeTimeout      time.Duration  // 写入超时
		idleTimeout       time.Duration  // 空闲超时
		processingTimeout time.Duration  // 处理超时
		shutdownTimeout   time.Duration  // 关闭超时
		onRequest         *lua.LFunction // 请求记录
		onError           *lua.LFunction // 服务错误
		onSuccess         *lua.LFunction // 服务成功
		onShutdown        *lua.LFunction // 服务关闭
	}
	ResError struct {
		statusCode int
		err        error
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
	cfg := &ServerConfig{
		logLevel:          "info",
		addr:              ":3000",
		workers:           100,
		readTimeout:       15 * time.Second,
		writeTimeout:      30 * time.Second,
		idleTimeout:       120 * time.Second,
		processingTimeout: 30 * time.Second,
		shutdownTimeout:   60 * time.Second,
	}

	if L.GetTop() >= 1 {
		opts := L.CheckTable(1)
		cfg = getServerConfig(L, opts, cfg)
	}

	server := &Server{
		Module:     util.NewModule(L),
		node:       newNode(),
		config:     cfg,
		semaphore:  semaphore.NewWeighted(cfg.workers),
		signalChan: make(chan os.Signal, 1),
	}

	server.initSignalHandling()
	methods := extendMethod(server)
	server.SetMethods(methods, util.Methods{
		"group":       server.Group,
		"listen":      server.Listen,
		"shutdown":    server.Shutdown,
		"stripPrefix": server.StripPrefix,
	})

	return server.Self()
}

func (s *Server) initSignalHandling() {
	s.signalOnce.Do(func() {
		signal.Notify(s.signalChan, os.Interrupt, syscall.SIGTERM)
	})
}

// creates a new route group with a common prefix and inherits middlewares.
func (s *Server) Group(L *lua.LState) int {
	pattern := L.CheckString(1)
	s.mu.Lock()
	middlewares := append([]Handler{}, s.middlewares...)
	s.mu.Unlock()
	group := &Server{
		Module:      util.NewModule(L),
		prefix:      s.pathJoin(pattern),
		node:        s.node,
		middlewares: middlewares,
		config:      s.config,
	}
	methods := extendMethod(group)
	return group.SetMethods(methods).Self()
}

// Use adds middleware handlers to the server.
func (s *Server) Use(L *lua.LState) int {
	n := L.GetTop()
	if n == 0 {
		return s.Self()
	}
	middlewares := make([]Handler, 0, n)
	for i := 1; i <= n; i++ {
		middlewares[i-1] = L.CheckFunction(i)
	}
	s.mu.Lock()
	s.middlewares = append(s.middlewares, middlewares...)
	s.mu.Unlock()
	return s.Self()
}

// registers a handler for a specific HTTP method and path.
func (s *Server) handle(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		path := s.pathJoin(L.CheckString(1))
		handler := s.applyMiddlewares(L.CheckFunction(2))
		method = strings.ToUpper(method)
		if err := s.node.add(method, path, handler); err != nil {
			L.RaiseError("failed to add route: %v", err)
		}
		return s.Self()
	}
}

// listen starts the HTTP server on the specified address.
func (s *Server) Listen(L *lua.LState) int {
	addr := s.config.addr
	if L.GetTop() > 0 {
		addr = L.CheckString(1)
		s.config.addr = addr
	}

	// start creates the listener and starts serving HTTP requests.
	listener, err := s.getListener(addr)
	if err != nil {
		err = fmt.Errorf("server start error: %w", err)
		s.logger("error", err)
		L.Push(lua.LString(err.Error()))
		return 1
	}

	s.httpServer = &http.Server{
		Handler:      s,
		Addr:         addr,
		ReadTimeout:  s.config.readTimeout,
		WriteTimeout: s.config.writeTimeout,
		IdleTimeout:  s.config.idleTimeout,
	}

	go func() {
		err := s.httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			err = fmt.Errorf("server start error: %w", err)
			s.logger("error", err)
		}
	}()

	s.logger("success", fmt.Sprintf("server started on %v", addr))

	// keep the server running until shutdown
	sig := <-s.signalChan
	s.shutdown(sig.String())
	return 0
}

// creates a net.Listener based on the server configuration (HTTP or HTTPS).
func (s *Server) getListener(addr string) (net.Listener, error) {
	if s.config.certFile == "" || s.config.keyFile == "" {
		return net.Listen("tcp", addr)
	}
	cert, err := tls.LoadX509KeyPair(s.config.certFile, s.config.keyFile)
	if err != nil {
		return nil, err
	}
	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	return tls.Listen("tcp", addr, config)
}

// Shutdown initiates the graceful shutdown of the server from Lua.
func (s *Server) Shutdown(L *lua.LState) int {
	s.shutdown("")
	return 0
}

// shutdown initiates the graceful shutdown of the server.
func (s *Server) shutdown(sig string) {

	if sig != "" {
		s.logger("shutdown", fmt.Sprintf("received signal: %v", sig))
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.shutdownTimeout)
	defer cancel()

	s.httpServer.SetKeepAlivesEnabled(false)
	if err := s.httpServer.Close(); err != nil {
		s.logger("error", fmt.Sprintf("server close error: %v", err))
		return
	}

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger("error", fmt.Sprintf("server shutdown error: %v", err))
	} else {
		s.logger("shutdown", "server stopped gracefully")
	}
}

func (s *Server) responseLog(ctx *Context, statusCode int, err error) {
	if err != nil {
		if s.config.logLevel == "error" {
			ctx.error(statusCode, err)
		} else {
			ctx.error(statusCode, nil)
		}
	}
	s.logger("request", ctx)
}

// ServeHTTP handles incoming HTTP requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(w, r)
	ctx.errorTemplate = s.config.errorTemplate

	defer func() {
		if rec := recover(); rec != nil {
			err := fmt.Errorf("panic recovered: %v", rec)
			s.responseLog(ctx, http.StatusInternalServerError, err)
		}
		ctx.Release()
	}()

	// Acquire semaphore for concurrency control
	if err := s.semaphore.Acquire(r.Context(), 1); err != nil {
		s.responseLog(ctx, http.StatusInternalServerError, err)
		return
	}
	defer s.semaphore.Release(1)

	// Request timeout context
	timeout := s.config.processingTimeout
	timeoutCtx, cancel := context.WithTimeout(ctx.request.Context(), timeout)
	defer cancel()

	// Execute handler asynchronously
	done := make(chan ResError, 1)
	go func() {
		defer close(done)
		handler, params, statusCode, err := s.node.find(ctx.request)
		if err == nil {
			ctx.params = params
			w, r := ctx.getResponseLuaApi(s.Vm), ctx.getRequestLuaApi(s.Vm)
			if e := util.CallLua(s.Vm, handler, w, r); e != nil {
				statusCode = http.StatusInternalServerError
				err = e
			}
		}
		done <- ResError{statusCode: statusCode, err: err}
	}()

	select {
	case res := <-done:
		s.responseLog(ctx, res.statusCode, res.err)
	case <-timeoutCtx.Done():
		ctx.timedOut = true
		err := errors.New("request processing timeout")
		s.responseLog(ctx, http.StatusRequestTimeout, err)
	}
}

// strip a prefix from the request path.
func (s *Server) StripPrefix(L *lua.LState) int {
	prefix, handler := L.CheckString(1), L.CheckFunction(2)

	fn := func(l *lua.LState) int {
		w, r := l.CheckTable(1), l.CheckTable(2)

		path := r.RawGetString("path").String()
		if strings.HasPrefix(path, prefix) {
			newPath := strings.TrimPrefix(path, prefix)
			setPath, _ := r.RawGetString("setPath").(*lua.LFunction)
			if err := util.CallLua(l, setPath, lua.LString(newPath)); err != nil {
				l.RaiseError("http.Request setPath failed: %s", err)
			}
		}

		if err := util.CallLua(l, handler, w, r); err != nil {
			l.RaiseError(err.Error())
		}
		return 0
	}

	L.Push(L.NewFunction(fn))
	return 1
}

// applies the server-level middlewares to a given handler.
func (s *Server) applyMiddlewares(handler Handler) Handler {
	if len(s.middlewares) == 0 {
		return handler
	}

	s.mu.Lock()
	middlewares := append([]Handler{}, s.middlewares...)
	s.mu.Unlock()

	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = s.wrapMiddleware(middlewares[i], handler)
	}

	return handler
}

func (s *Server) wrapMiddleware(middleware, next *lua.LFunction) Handler {
	return s.Vm.NewFunction(func(L *lua.LState) int {
		w, r := L.CheckTable(1), L.CheckTable(2)

		// Pass the next handler as the third argument
		nextFn := L.NewFunction(func(l *lua.LState) int {
			if err := util.CallLua(l, next, w, r); err != nil {
				L.RaiseError(err.Error())
			}
			return 0
		})

		// Call the middleware, passing the next handler as an argument
		if err := util.CallLua(L, middleware, w, r, nextFn); err != nil {
			L.RaiseError(err.Error())
		}
		return 0
	})
}

// joins server prefix with the given pattern.
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

// parses the server configuration from a Lua table.
func getServerConfig(L *lua.LState, opts *lua.LTable, cfg *ServerConfig) *ServerConfig {
	opts.ForEach(func(k lua.LValue, v lua.LValue) {
		key := k.String()
		switch key {
		case "logLevel":
			if val, ok := util.CheckString(L, key, v); ok {
				cfg.logLevel = val
			}
		case "certFile":
			if val, ok := util.CheckString(L, key, v); ok {
				cfg.certFile = val
			}
		case "keyFile":
			if val, ok := util.CheckString(L, key, v); ok {
				cfg.keyFile = val
			}
		case "readTimeout":
			if val, ok := util.CheckDuration(L, key, v); ok {
				cfg.readTimeout = val
			}
		case "writeTimeout":
			if val, ok := util.CheckDuration(L, key, v); ok {
				cfg.writeTimeout = val
			}
		case "idleTimeout":
			if val, ok := util.CheckDuration(L, key, v); ok {
				cfg.idleTimeout = val
			}
		case "processingTimeout":
			if val, ok := util.CheckDuration(L, key, v); ok {
				cfg.processingTimeout = val
			}
		case "shutdownTimeout":
			if val, ok := util.CheckDuration(L, key, v); ok {
				cfg.shutdownTimeout = val
			}
		case "workers":
			if val, ok := util.CheckInt64(L, key, v); ok {
				cfg.workers = val
			}
		case "onRequest":
			if val, ok := util.CheckFunction(L, key, v); ok {
				cfg.onRequest = val
			}
		case "onError":
			if val, ok := util.CheckFunction(L, key, v); ok {
				cfg.onError = val
			}
		case "onSuccess":
			if val, ok := util.CheckFunction(L, key, v); ok {
				cfg.onSuccess = val
			}
		case "onShutdown":
			if val, ok := util.CheckFunction(L, key, v); ok {
				cfg.onShutdown = val
			}
		case "errorTemplate":
			if val, ok := util.CheckString(L, key, v); ok {
				cfg.errorTemplate = val
			}
		}
	})
	return cfg
}
