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
	*util.Module
	router     *Router
	httpServer *http.Server
	config     *serverConfig
}

type serverConfig struct {
	certFile     string
	keyFile      string
	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
	error        *lua.LFunction
	success      *lua.LFunction
	shutdown     *lua.LFunction
}

func extendMethod(mod *Server) util.Methods {
	return util.Methods{
		"handle":  mod.handle,
		"use":     mod.use,
		"any":     mod.method(`*`),
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
}

func ServerLoader(L *lua.LState) *lua.LTable {
	mod := &Server{
		Module: util.NewModule(L, nil),
		router: newRouter(),
	}
	mod.SetMethods(extendMethod(mod), util.Methods{
		"listen":   mod.listen,
		"shutdown": mod.shutdown,
		"group":    mod.group,
	})

	return mod.Method
}

func (s *Server) group(L *lua.LState) int {
	pattern := L.CheckString(1)
	mod := &Server{
		Module: util.NewModule(L, nil),
		router: s.router.group(pattern),
	}
	methods := extendMethod(mod)
	return mod.SetMethods(methods).Self()
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
	path, handler := s.Vm.CheckString(1), s.Vm.CheckFunction(2)
	s.router.method(method).handle(path, s.createLuaHandler(handler))
	return s.Self()
}

func (s *Server) createLuaHandler(handler *lua.LFunction) Handler {
	return func(ctx *Context) string {
		s.Vm.SetTop(0) // 清空栈
		ctxApi := ctx.getLuaApi(s.Vm)
		if err := util.CallLua(s.Vm, handler, ctxApi); err != nil {
			ctx.Error(err.Error(), http.StatusInternalServerError)
		}
		ret := s.Vm.Get(-1).String()
		s.Vm.Pop(1)
		return ret
	}
}

func (s *Server) listen(L *lua.LState) int {
	addr, opts := L.CheckString(1), L.OptTable(2, L.NewTable())

	callback := L.NewFunction(func(l *lua.LState) int {
		fmt.Println(l.CheckString(1))
		return 0
	})

	config := serverConfig{
		readTimeout:  30 * time.Second,
		writeTimeout: 60 * time.Second,
		idleTimeout:  90 * time.Second,
		error:        callback,
		success:      callback,
		shutdown:     callback,
	}

	opts.ForEach(func(k lua.LValue, v lua.LValue) {

		if k.String() == `certFile` {
			if val, ok := v.(lua.LString); ok {
				config.certFile = val.String()
			} else {
				L.ArgError(2, "certFile must be string")
			}
		}
		if k.String() == `keyFile` {
			if val, ok := v.(lua.LString); ok {
				config.keyFile = val.String()
			} else {
				L.ArgError(2, "keyFile must be number")
			}
		}
		if k.String() == `readTimeout` {
			if val, ok := v.(lua.LNumber); ok {
				config.readTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(2, "readTimeout must be number(time second)")
			}
		}
		if k.String() == `writeTimeout` {
			if val, ok := v.(lua.LNumber); ok {
				config.writeTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(2, "writeTimeout must be number(time second)")
			}
		}
		if k.String() == `idleTimeout` {
			if val, ok := v.(lua.LNumber); ok {
				config.idleTimeout = time.Duration(int(val)) * time.Second
			} else {
				L.ArgError(2, "idleTimeout must be number(time second)")
			}
		}
		if k.String() == `error` {
			if val, ok := v.(*lua.LFunction); ok {
				config.error = val
			} else {
				L.ArgError(2, "error must be function")
			}
		}
		if k.String() == `success` {
			if val, ok := v.(*lua.LFunction); ok {
				config.success = val
			} else {
				L.ArgError(2, "success must be function")
			}
		}
		if k.String() == `shutdown` {
			if val, ok := v.(*lua.LFunction); ok {
				config.shutdown = val
			} else {
				L.ArgError(2, "shutdown must be function")
			}
		}
	})
	s.config = &config

	server := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  config.readTimeout,
		WriteTimeout: config.writeTimeout,
		IdleTimeout:  config.idleTimeout,
	}
	s.httpServer = server

	serverReadyChan := make(chan struct{})
	serverErrorChan := make(chan error, 1)
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM)

	var listener net.Listener
	var serverError error

	if config.certFile != "" && config.keyFile != "" {
		if cert, err := tls.LoadX509KeyPair(config.certFile, config.keyFile); err != nil {
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
		s.callLua(config.success, "server is listening on "+addr)
	case err := <-serverErrorChan:
		if err != nil {
			s.callLua(config.error, err.Error())
			return 0
		}
	}

	select {
	case sig := <-interruptChan:
		fmt.Printf("signal: %v\n", sig)
	case err := <-serverErrorChan:
		s.callLua(config.error, err.Error())
	}

	s.shutdown(L)
	return 0
}

func (s *Server) shutdown(L *lua.LState) int {
	if s.httpServer == nil {
		s.callLua(s.config.error, "service not started")
		os.Exit(1)
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.callLua(s.config.error, err.Error())
		os.Exit(1)
	} else {
		s.callLua(s.config.shutdown, "server gracefully stopped")
		os.Exit(0)
	}

	return 0
}

func (s *Server) callLua(callback *lua.LFunction, args ...interface{}) {
	if err := util.CallLua(s.Vm, callback, args...); err != nil {
		s.Vm.RaiseError("callback execution error: %v", err)
	}
}
