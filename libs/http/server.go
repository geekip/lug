package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Server struct {
	util.Module
	router *Router
}

func ServerLoader(L *lua.LState) *lua.LTable {
	mod := newServer(L, newRouter())
	mod.Fn.RawSetString("listen", L.NewFunction(mod.listen))
	mod.Fn.RawSetString("group", L.NewFunction(mod.group))
	return mod.Fn
}

func newServer(L *lua.LState, router *Router) *Server {
	mod := &Server{
		Module: *util.GetModule(L),
		router: router,
	}
	api := util.LGFunctions{
		"handle":  mod.handle,
		"use":     mod.use,
		"any":     mod.method("*"),
		"connect": mod.method(http.MethodConnect),
		"delete":  mod.method(http.MethodDelete),
		"get":     mod.method(http.MethodGet),
		"head":    mod.method(http.MethodHead),
		"options": mod.method(http.MethodOptions),
		"patch":   mod.method(http.MethodPatch),
		"post":    mod.method(http.MethodPost),
		"put":     mod.method(http.MethodPut),
		"trace":   mod.method(http.MethodTrace),
	}
	mod.SetFuncs(api)
	return mod
}

func (s *Server) group(L *lua.LState) int {
	pattern := L.CheckString(1)
	mod := newServer(L, s.router.group(pattern))
	return mod.Self()
}

func (s *Server) use(L *lua.LState) int {
	n := L.GetTop()
	handlers := make([]Handler, n)
	for i := 1; i <= n; i++ {
		handler := L.CheckFunction(i)
		handlers[i-1] = s.createLuaHandler(handler)
	}
	s.router.use(handlers...)
	return s.Self()
}

func (s *Server) method(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		return s.handleRoute(method)
	}
}

func (s *Server) handle(L *lua.LState) int {
	return s.handleRoute(L.CheckString(1))
}

func (s *Server) handleRoute(method string) int {
	path := s.Vm.CheckString(1)
	handler := s.Vm.CheckFunction(2)
	s.router.method(method).handle(path, s.createLuaHandler(handler))
	return s.Self()
}

func (s *Server) createLuaHandler(handler *lua.LFunction) Handler {
	return func(ctx *Context) string {
		s.Vm.SetTop(0) // 清空栈
		ctxApi := ctx.newContextApi(s.Vm)
		if err := util.CallLua(s.Vm, handler, ctxApi); err != nil {
			ctx.Error(err.Error(), http.StatusInternalServerError)
		}
		return s.Vm.Get(-1).String()
	}
}

func (s *Server) listen(L *lua.LState) int {
	addr := L.CheckString(1)
	opts := L.CheckTable(2)

	certFile := util.GetStringFromTable(L, opts, "cert")
	keyFile := util.GetStringFromTable(L, opts, "key")
	readTimeout := time.Duration(util.GetIntFromTable(L, opts, "readTimeout"))
	writeTimeout := time.Duration(util.GetIntFromTable(L, opts, "writeTimeout"))
	idleTimeout := time.Duration(util.GetIntFromTable(L, opts, "idleTimeout"))

	server := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  readTimeout * time.Millisecond,
		WriteTimeout: writeTimeout * time.Millisecond,
		IdleTimeout:  idleTimeout * time.Millisecond,
	}

	serverReadyChan := make(chan struct{})
	serverErrorChan := make(chan error, 1)
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM)

	var listener net.Listener
	var serverError error

	// 处理TLS配置
	if certFile != "" && keyFile != "" {
		if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
			serverErrorChan <- err
		} else {
			listener, serverError = tls.Listen("tcp", addr, &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS12,
			})
		}
	} else {
		listener, serverError = net.Listen("tcp", addr)
	}

	go func() {
		if serverError != nil {
			serverErrorChan <- serverError
			return
		}
		close(serverReadyChan)

		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			serverErrorChan <- err
		} else {
			serverErrorChan <- nil
		}
	}()

	select {
	case <-serverReadyChan:
		callLua(L, opts, "success", "server is listening on "+addr)
	case err := <-serverErrorChan:
		if err != nil {
			callLua(L, opts, "error", err.Error())
			return 0
		}
	}

	select {
	case sig := <-interruptChan:
		fmt.Printf("signal: %v\n", sig)
	case err := <-serverErrorChan:
		callLua(L, opts, "error", err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err == nil {
		callLua(L, opts, "shutdown", "server gracefully stopped")
	}

	return 0
}

func callLua(L *lua.LState, opts *lua.LTable, name string, args ...interface{}) {
	cb := L.GetField(opts, name)
	if cb.Type() == lua.LTNil {
		return
	}
	if err := util.CallLua(L, cb, args...); err != nil {
		L.RaiseError("callback execution error: %v", err)
	}
}
