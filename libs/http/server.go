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
		methods     []string
		node        *Node
		middlewares []Handler
		// ctx         *Context
		config     *ServerConfig
		httpServer *http.Server
		semaphore  *semaphore.Weighted
		SigChan    chan os.Signal
	}
	ServerConfig struct {
		Debug             bool
		CertFile          string         // 证书文件
		KeyFile           string         // 私钥文件
		Addr              string         // 监听地址
		errorTemplate     string         // 错误模板
		Workers           int64          // 最大并发
		ReadTimeout       time.Duration  // 读取超时
		WriteTimeout      time.Duration  // 写入超时
		IdleTimeout       time.Duration  // 空闲超时
		ProcessingTimeout time.Duration  // 处理超时
		ShutdownTimeout   time.Duration  // 关闭超时
		onRequest         *lua.LFunction // 请求记录
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
	cfg := &ServerConfig{
		Debug:             true,
		Addr:              ":3000",
		Workers:           100,
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

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	server := &Server{
		Module:    util.NewModule(L),
		node:      newNode(),
		config:    cfg,
		semaphore: semaphore.NewWeighted(cfg.Workers),
		SigChan:   sigChan,
	}

	methods := extendMethod(server)
	server.SetMethods(methods, util.Methods{
		"group":    server.Group,
		"listen":   server.Listen,
		"shutdown": server.Shutdown,
	})

	return server.Self()
}

func (s *Server) Group(L *lua.LState) int {
	pattern := L.CheckString(1)
	s.mu.Lock()
	defer s.mu.Unlock()
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
	handlers := make([]Handler, n)
	for i := 1; i <= n; i++ {
		handlers[i-1] = L.CheckFunction(i)
	}
	s.mu.Lock()
	s.middlewares = append(s.middlewares, handlers...)
	s.mu.Unlock()
	return s.Self()
}

func (s *Server) handle(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		path := s.pathJoin(L.CheckString(1))
		handler := L.CheckFunction(2)

		s.mu.Lock()
		methods := append([]string{}, s.methods...)
		methods = append(methods, method)
		s.methods = nil
		s.mu.Unlock()

		for _, m := range methods {
			mUpper := strings.ToUpper(m)
			handler = s.applyMiddlewares(handler)
			if err := s.node.add(mUpper, path, handler); err != nil {
				L.RaiseError(err.Error())
			}
		}
		return s.Self()
	}
}

func (s *Server) Listen(L *lua.LState) int {
	s.config.Addr = L.CheckString(1)
	if err := s.start(); err != nil {
		L.Push(lua.LString(err.Error()))
		s.logger("error", err)
		return 1
	}

	// Wait for Lua context to be done as well
	go func() {
		ctx := L.Context()
		if ctx != nil {
			<-ctx.Done()
			process, err := os.FindProcess(os.Getpid())
			if err != nil {
				return
			}
			err = process.Signal(syscall.SIGINT)
			if err != nil {
				return
			}
			s.shutdown()
		}
	}()

	s.waitForInterrupt()
	return 0
}

func (s *Server) waitForInterrupt() {
	sig := <-s.SigChan
	s.logger("shutdown", fmt.Sprintf("Received signal: %v", sig))
	s.shutdown()
}

// shutdown initiates the graceful shutdown of the server.
func (s *Server) shutdown() {
	s.httpServer.SetKeepAlivesEnabled(false)

	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.httpServer.Close(); err != nil {
		s.logger("error", fmt.Sprintf("Shutdown error: %v", err))
		return
	}

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger("error", fmt.Sprintf("Shutdown error: %v", err))
	} else {
		s.logger("shutdown", "server gracefully stopped")
	}
}

func (s *Server) Shutdown(L *lua.LState) int {
	s.shutdown()
	return 0
}

func (s *Server) start() error {
	listener, err := s.getListener(s.config.Addr)
	if err != nil {
		return err
	}

	s.httpServer = &http.Server{
		Addr:         s.config.Addr,
		Handler:      s,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}
	s.logger("success", fmt.Sprintf("Server started on %v", s.config.Addr))

	go func() {
		if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger("error", fmt.Sprintf("Server error: %v", err))
		}
	}()

	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(w, r)
	ctx.ErrorTemplate = s.config.errorTemplate
	defer func() {
		s.logger("request", ctx)
		ctx.Release()
	}()

	if err := s.semaphore.Acquire(r.Context(), 1); err != nil {
		ctx.setStatus(http.StatusServiceUnavailable, err)
		return
	}
	defer s.semaphore.Release(1)

	s.processRequest(ctx)
}

// 请求处理
func (s *Server) processRequest(ctx *Context) {

	defer func() {
		if rec := recover(); rec != nil {
			err := fmt.Errorf("panic recovered: %v", rec)
			ctx.setStatus(http.StatusInternalServerError, err)
		}
	}()

	// 超时控制上下文
	timeoutCtx, cancel := context.WithTimeout(ctx.r.Context(), s.config.ProcessingTimeout)
	defer cancel()

	// 使用通道接收处理结果
	done := make(chan struct{})
	go func() {
		defer close(done)

		handler, params, statusCode, err := s.node.find(ctx.r)
		ctx.setStatus(statusCode, err)

		if err == nil {
			ctx.Params = params
			w, r := ctx.getResponseLuaApi(s.Vm), ctx.getRequestLuaApi(s.Vm)
			if err := util.CallLua(s.Vm, handler, w, r); err != nil {
				ctx.setStatus(http.StatusInternalServerError, err)
			}
		}
	}()

	select {
	case <-done:
		// Request processed successfully or with an error
	case <-timeoutCtx.Done():
		ctx.TimedOut = true
		ctx.setStatus(http.StatusRequestTimeout, errors.New("the service timed out while processing the request. Please try again later or check your network connection"))
	}
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

func (s *Server) applyMiddlewares(handler Handler) Handler {
	if len(s.middlewares) == 0 {
		return handler
	}
	s.mu.Lock()
	middlewares := append([]Handler{}, s.middlewares...)
	middlewares = append(middlewares, handler)
	s.mu.Unlock()

	// Create a chain of Go functions that call the Lua middlewares
	var nextHandler Handler = nil
	count := len(middlewares) - 1
	for i := count; i >= 0; i-- {
		isFinal := i == count
		// Capture the current nextHandler in the closure
		next := nextHandler
		nextHandler = s.gethandler(middlewares[i], next, isFinal)
	}
	return nextHandler
}

func (s *Server) gethandler(middleware, next Handler, isFinal bool) Handler {
	handler := s.Vm.NewFunction(func(L *lua.LState) int {
		w, r := L.CheckTable(1), L.CheckTable(2)

		// Call the final handler
		if isFinal {
			if err := util.CallLua(L, middleware, w, r); err != nil {
				L.RaiseError(err.Error())
			}
			return 0
		}

		// Call the middleware, passing the next handler as an argument
		fn := func(l *lua.LState) int {
			if next != nil {
				if err := util.CallLua(l, next, w, r); err != nil {
					L.RaiseError(err.Error())
				}
			}
			return 0
		}
		lNext := L.NewFunction(fn)
		if err := util.CallLua(L, middleware, w, r, lNext); err != nil {
			L.RaiseError(err.Error())
		}
		return 0
	})
	return handler
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

func getServerConfig(L *lua.LState, opts *lua.LTable, cfg *ServerConfig) *ServerConfig {
	opts.ForEach(func(k lua.LValue, v lua.LValue) {
		key := k.String()
		switch key {
		case "debug":
			if val, ok := util.ArgLBool(L, key, v); ok {
				cfg.Debug = val
			}
		case "certFile":
			if val, ok := util.ArgLString(L, key, v); ok {
				cfg.CertFile = val
			}
		case "keyFile":
			if val, ok := util.ArgLString(L, key, v); ok {
				cfg.KeyFile = val
			}
		case "readTimeout":
			if val, ok := util.ArgLDuration(L, key, v); ok {
				cfg.ReadTimeout = val
			}
		case "writeTimeout":
			if val, ok := util.ArgLDuration(L, key, v); ok {
				cfg.WriteTimeout = val
			}
		case "idleTimeout":
			if val, ok := util.ArgLDuration(L, key, v); ok {
				cfg.IdleTimeout = val
			}
		case "processingTimeout":
			if val, ok := util.ArgLDuration(L, key, v); ok {
				cfg.ProcessingTimeout = val
			}
		case "shutdownTimeout":
			if val, ok := util.ArgLDuration(L, key, v); ok {
				cfg.ShutdownTimeout = val
			}
		case "workers":
			if val, ok := util.ArgLInt64(L, key, v); ok {
				cfg.Workers = val
			}
		case "onRequest":
			if val, ok := util.ArgLFunction(L, key, v); ok {
				cfg.onRequest = val
			}
		case "onError":
			if val, ok := util.ArgLFunction(L, key, v); ok {
				cfg.onError = val
			}
		case "onSuccess":
			if val, ok := util.ArgLFunction(L, key, v); ok {
				cfg.onSuccess = val
			}
		case "onShutdown":
			if val, ok := util.ArgLFunction(L, key, v); ok {
				cfg.onShutdown = val
			}
		case "errorTemplate":
			if val, ok := util.ArgLString(L, key, v); ok {
				cfg.errorTemplate = val
			}
		}
	})
	return cfg
}
