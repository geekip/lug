package http

import (
	"fmt"
	"log"
	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

func (s *Server) logger(L *lua.LState, logType string, args ...interface{}) {

	if len(args) < 1 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	arg := args[0]
	var callback *lua.LFunction
	var logMessage string
	var hasMessage bool
	var luaArgs []lua.LValue

	switch logType {
	case "error":
		var err error
		err, hasMessage = arg.(error)
		if !hasMessage {
			return
		}
		callback = s.config.onError
		logMessage = err.Error()
		luaArgs = []lua.LValue{lua.LString(logMessage)}

	case "success":
		logMessage, hasMessage = arg.(string)
		if !hasMessage {
			return
		}
		callback = s.config.onSuccess
		luaArgs = []lua.LValue{lua.LString(logMessage)}

	case "shutdown":
		logMessage, hasMessage = arg.(string)
		if !hasMessage {
			return
		}
		callback = s.config.onShutdown
		luaArgs = []lua.LValue{lua.LString(logMessage)}

	case "request":

		if s.config.logLevel == "silent" {
			return
		}

		ctx, hasMessage := arg.(*Context)
		if !hasMessage {
			return
		}

		if s.config.logLevel == "error" && ctx.statusError == nil {
			return
		}

		callback = s.config.onRequest
		luaArgs = []lua.LValue{ctx.luaContext(L)}

		cip := ctx.remoteIP()
		tpl := "method: %s, code: %d, path: %s, time: %v, client: %s, server: %s"
		data := []interface{}{
			ctx.request.Method,
			ctx.statusCode,
			ctx.request.URL.Path,
			ctx.Since(),
			cip,
			s.config.addr,
		}

		if ctx.statusError != nil {
			if s.config.logLevel == "error" {
				d := append(data, ctx.statusError.Error())
				logMessage = fmt.Sprintf("[error] "+tpl+", (%s)", d...)
			} else {
				d := append(data, ctx.statusText)
				logMessage = fmt.Sprintf("[error] "+tpl+", (%s)", d...)
			}
		} else {
			logMessage = fmt.Sprintf("[success] "+tpl, data...)
		}

	default:
		log.Printf("logger: unknown log type: %s", logType)
		return
	}

	if callback == nil {
		// util.DebugPrintError(errors.New(logMessage))
		// L.RaiseError(logMessage)
		log.Println(logMessage)
		return
	}

	if err := util.CallLua(L, callback, luaArgs...); err != nil {
		log.Printf("logger: Lua callback error (%s): %v", logType, err)
	}
}
