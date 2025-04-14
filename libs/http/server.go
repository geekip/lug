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
		mu          sync.Mutex
		prefix      string
		route       *Route
		middlewares []Handler
		config      *ServerConfig
		httpServer  *http.Server
		semaphore   *semaphore.Weighted
		signalChan  chan os.Signal
		signalOnce  sync.Once
		api         *lua.LTable
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
	Status struct {
		StatusCode  int
		StatusText  string
		StatusError error
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

	instance := &Server{
		route:      newRoute(),
		config:     cfg,
		semaphore:  semaphore.NewWeighted(cfg.workers),
		signalChan: make(chan os.Signal, 1),
	}
	instance.initSignalHandling()

	methods := extendMethod(instance)
	api := util.SetMethods(L, methods, util.Methods{
		"group":    instance.Group,
		"listen":   instance.Listen,
		"shutdown": instance.Shutdown,
	})
	instance.api = api
	return util.Push(L, api)
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
		prefix:      s.pathJoin(pattern),
		route:       s.route,
		middlewares: middlewares,
		config:      s.config,
	}
	group.api = util.SetMethods(L, extendMethod(group))
	return util.Push(L, group.api)
}

// Use adds middleware handlers to the server.
func (s *Server) Use(L *lua.LState) int {
	n := L.GetTop()
	if n == 0 {
		return util.Push(L, s.api)
	}
	middlewares := make([]Handler, 0, n)
	for i := 1; i <= n; i++ {
		middlewares = append(middlewares, L.CheckFunction(i))
	}
	s.mu.Lock()
	s.middlewares = append(s.middlewares, middlewares...)
	s.mu.Unlock()
	return util.Push(L, s.api)
}

// registers a handler for a specific HTTP method and path.
func (s *Server) handle(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		path := s.pathJoin(L.CheckString(1))
		var stripPrefix string
		var handler *lua.LFunction

		switch v := L.CheckAny(2).(type) {
		case *lua.LFunction:
			handler = v
		case lua.LString:
			stripPrefix = v.String()
			handler = L.CheckFunction(3)
		default:
			L.ArgError(2, "must be a string or function")
		}

		if len(s.middlewares) > 0 {
			handler = s.applyMiddleware(L, handler)
		}
		method = strings.ToUpper(method)
		if err := s.route.add(method, path, stripPrefix, handler); err != nil {
			L.RaiseError("failed to add route: %v", err)
		}
		return util.Push(L, s.api)
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
		s.logger(L, "error", err)
		return util.Error(L, err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := newContext(w, r)
		defer func() {
			ctx.Release()
		}()
		ctx.errorTemplate = s.config.errorTemplate
		s.ServeHTTP(L, ctx)
	})

	s.httpServer = &http.Server{
		Handler:      handler,
		Addr:         addr,
		ReadTimeout:  s.config.readTimeout,
		WriteTimeout: s.config.writeTimeout,
		IdleTimeout:  s.config.idleTimeout,
	}

	go func() {
		err := s.httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger(L, "error", fmt.Errorf("server start error: %w", err))
		}
	}()

	s.logger(L, "success", fmt.Sprintf("server started on %v", addr))

	// keep the server running until shutdown
	sig := <-s.signalChan
	s.shutdown(L, sig.String())
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
	s.shutdown(L, "")
	return 0
}

// shutdown initiates the graceful shutdown of the server.
func (s *Server) shutdown(L *lua.LState, sig string) {

	if sig != "" {
		s.logger(L, "shutdown", fmt.Sprintf("received signal: %v", sig))
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.shutdownTimeout)
	defer cancel()

	s.httpServer.SetKeepAlivesEnabled(false)
	if err := s.httpServer.Close(); err != nil {
		s.logger(L, "error", fmt.Sprintf("server closed error: %v", err))
		return
	}

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger(L, "error", fmt.Sprintf("server shutdown error: %v", err))
	} else {
		s.logger(L, "shutdown", "server stopped gracefully")
	}
}

func (s *Server) responseLog(L *lua.LState, ctx *Context, statusCode int, err error) {
	// fmt.Println(err)
	if err != nil {
		if s.config.logLevel == "error" {
			ctx.error(statusCode, err)
		} else {
			ctx.error(statusCode, nil)
		}
	}
	ctx.statusError = err
	s.logger(L, "request", ctx)
}

// ServeHTTP handles incoming HTTP requests.
func (s *Server) ServeHTTP(L *lua.LState, ctx *Context) {

	// Request timeout context
	timeout := s.config.processingTimeout
	timeoutCtx, cancel := context.WithTimeout(ctx.request.Context(), timeout)
	defer cancel()

	// Execute handler asynchronously
	done := make(chan *Status, 1)

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				err := fmt.Errorf("panic recovered: %v", rec)
				done <- &Status{
					StatusCode:  http.StatusInternalServerError,
					StatusError: err,
				}
			}
		}()

		// Acquire semaphore for concurrency control
		if err := s.semaphore.Acquire(timeoutCtx, 1); err != nil {
			done <- &Status{
				StatusCode:  http.StatusServiceUnavailable,
				StatusError: fmt.Errorf("concurrency limit: %w", err),
			}
			return
		}
		defer s.semaphore.Release(1)

		stripPrefix, handler, params, code, routeErr := s.route.find(ctx.request)
		if routeErr != nil {
			done <- &Status{
				StatusCode:  code,
				StatusError: routeErr,
			}
			return
		}

		ctx.params = params
		if stripPrefix != "" {
			ctx.stripPrefix(stripPrefix)
		}

		vm := util.VmPool.Clone(L)
		defer util.VmPool.Put(vm)

		if err := util.CallLua(vm, handler, ctx.luaContext(vm)); err != nil {
			done <- &Status{
				StatusCode:  http.StatusInternalServerError,
				StatusError: fmt.Errorf("lua execution error: %w", err),
			}
			return
		}

		done <- &Status{StatusCode: http.StatusOK}
	}()

	select {
	case status := <-done:
		s.responseLog(L, ctx, status.StatusCode, status.StatusError)
	case <-timeoutCtx.Done():
		ctx.timedOut = true
		err := errors.New("request processing timeout")
		s.responseLog(L, ctx, http.StatusRequestTimeout, err)
	}
}

// applies the server-level middlewares to a given handler.
func (s *Server) applyMiddleware(L *lua.LState, handler *lua.LFunction) Handler {
	s.mu.Lock()
	count := len(s.middlewares)
	middlewares := make([]Handler, count)
	copy(middlewares, s.middlewares)
	s.mu.Unlock()

	for i := count - 1; i >= 0; i-- {
		handler = s.wrapMiddleware(L, middlewares[i], handler)
	}
	return handler
}

func (s *Server) wrapMiddleware(L *lua.LState, middleware, next *lua.LFunction) Handler {
	return L.NewFunction(func(l *lua.LState) int {
		ctx := l.CheckTable(1)
		ctx.RawSetString("next", s.wrapNextHandler(l, next, ctx))
		if err := util.CallLua(l, middleware, ctx); err != nil {
			l.RaiseError("middleware execution: %v", err)
		}
		return 0
	})
}

func (s *Server) wrapNextHandler(L *lua.LState, next *lua.LFunction, ctx *lua.LTable) *lua.LFunction {
	var nextGuard sync.Once
	return L.NewFunction(func(l *lua.LState) int {
		nextGuard.Do(func() {
			ctx.RawSetString("next", lua.LNil)
			if err := util.CallLua(l, next, ctx); err != nil {
				l.RaiseError("next handler: %v", err)
			}
		})
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
