package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/geekip/lug/util"
	lua "github.com/yuin/gopher-lua"
)

type Server struct {
	router *Router
}

var statePool = sync.Pool{
	New: func() interface{} {
		return lua.NewState()
	},
}

func Loader(L *lua.LState) *lua.LTable {
	server := newServer(newRouter())
	mod := server.createMod(L)
	mod.RawSetString("listen", L.NewFunction(server.listen))
	mod.RawSetString("group", L.NewFunction(server.group))
	return mod
}

func (s *Server) createMod(L *lua.LState) *lua.LTable {
	mod := L.NewTable()
	api := map[string]lua.LGFunction{
		"handle":  s.handle,
		"use":     s.use,
		"any":     s.method("*"),
		"connect": s.method(http.MethodConnect),
		"delete":  s.method(http.MethodDelete),
		"get":     s.method(http.MethodGet),
		"head":    s.method(http.MethodHead),
		"options": s.method(http.MethodOptions),
		"patch":   s.method(http.MethodPatch),
		"post":    s.method(http.MethodPost),
		"put":     s.method(http.MethodPut),
		"trace":   s.method(http.MethodTrace),
	}
	for name, fn := range api {
		mod.RawSetString(name, L.NewFunction(fn))
	}
	return mod
}

func newServer(router *Router) *Server {
	return &Server{router: router}
}

func (s *Server) group(L *lua.LState) int {
	pattern := L.CheckString(1)
	server := newServer(s.router.group(pattern))
	mod := server.createMod(L)
	L.Push(mod)
	return 1
}

func (s *Server) method(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		return s.handleRoute(L, method)
	}
}

func (s *Server) handle(L *lua.LState) int {
	return s.handleRoute(L, L.CheckString(1))
}

func (s *Server) handleRoute(L *lua.LState, method string) int {
	path := L.CheckString(1)
	handler := L.CheckFunction(2)
	s.router.method(method).handle(path, s.createLuaHandler(L, handler))
	return 0
}

func (s *Server) use(L *lua.LState) int {
	n := L.GetTop()
	handlers := make([]Handler, n)
	for i := 1; i <= n; i++ {
		handler := L.CheckFunction(i)
		handlers[i-1] = s.createLuaHandler(L, handler)
	}
	s.router.use(handlers...)
	return 0
}

func (s *Server) createLuaHandler(L *lua.LState, handler *lua.LFunction) Handler {
	return func(ctx *Context) string {
		state := statePool.Get().(*lua.LState)
		defer func() {
			state.SetTop(0) // 清空栈
			statePool.Put(state)
		}()

		ctxApi := ctx.newContextApi(L)
		if err := util.CallLua(state, handler, ctxApi); err != nil {
			ctx.Error(err.Error(), http.StatusInternalServerError)
		}

		return state.Get(-1).String()
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
