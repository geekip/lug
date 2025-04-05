package http

import (
	"fmt"
	"log"
	"net/http"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func (s *Server) logger(logType string, args ...interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var callback *lua.LFunction
	var logMessage string
	var ok bool
	var luaArgs []lua.LValue

	switch logType {
	case "error":
		callback = s.config.onError
		if len(args) < 1 {
			return
		}
		var err error
		err, ok = args[0].(error)
		if !ok {
			return
		}
		logMessage = err.Error()
		luaArgs = []lua.LValue{lua.LString(logMessage)}

	case "success":
		callback = s.config.onSuccess
		if len(args) < 1 {
			return
		}
		logMessage, ok = args[0].(string)
		if !ok {
			return
		}
		luaArgs = []lua.LValue{lua.LString(logMessage)}

	case "shutdown":
		callback = s.config.onShutdown
		if len(args) < 1 {
			return
		}
		logMessage, ok = args[0].(string)
		if !ok {
			return
		}
		luaArgs = []lua.LValue{lua.LString(logMessage)}

	case "request":
		return
		callback = s.config.onRequest
		if len(args) < 1 {
			return
		}
		ctx, ok := args[0].(*Context)
		if !ok {
			return
		}

		cip := ctx.getClientIp()
		tpl := "method: %s, code: %d, path: %s, time: %v, client: %s, server: %s"
		data := []interface{}{
			ctx.r.Method,
			ctx.StatusCode,
			ctx.r.URL.Path,
			ctx.Since(),
			cip,
			s.config.Addr,
		}

		if ctx.err != nil {
			d := append(data, ctx.err.Error())
			logMessage = fmt.Sprintf("[error] "+tpl+", (%s)", d...)
		} else {
			logMessage = fmt.Sprintf("[success] "+tpl, data...)
		}

		// logMessage = fmt.Sprintf(
		// 	"%s %d - %s (%v) - %s",
		// 	ctx.r.Method, ctx.StatusCode, ctx.r.URL.Path, ctx.Since(), ctx.err.Error(),
		// )

		if ctx.err != nil {
			// logMessage = ctx.err.Error()
			if s.config.Debug {
				ctx.error(ctx.StatusCode, ctx.err)
			} else {
				ctx.error(ctx.StatusCode, nil)
			}
		}

	default:
		log.Printf("Unknown log type: %s", logType)
		return
	}

	if callback == nil {
		// util.DebugPrintError(errors.New(logMessage))
		log.Println(logMessage)
		return
	}

	L := s.Vm

	if logType == "request" {
		req := args[0].(*http.Request)
		statusCode := args[1].(int)
		duration := args[2].(time.Duration)

		tbl := L.NewTable()
		L.SetField(tbl, "method", lua.LString(req.Method))
		L.SetField(tbl, "path", lua.LString(req.URL.Path))
		L.SetField(tbl, "status", lua.LNumber(statusCode))
		L.SetField(tbl, "duration_ms", lua.LNumber(duration.Milliseconds()))
		luaArgs = []lua.LValue{tbl}
	}

	if err := L.CallByParam(lua.P{
		Fn:      callback,
		NRet:    0,
		Protect: true,
	}, luaArgs...); err != nil {
		log.Printf("Lua callback error (%s): %v", logType, err)
	}
}
