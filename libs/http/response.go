package http

import (
	"errors"
	"net/http"
	"text/template"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

func (ctx *Context) getResponseLuaApi(L *lua.LState) *lua.LTable {
	methods := util.NewModule(L, util.Methods{
		"write":     ctx.Write,
		"setStatus": ctx.SetStatus,
		"setHeader": ctx.SetHeader,
		"delHeader": ctx.DelHeader,
		"setCookie": ctx.SetCookie,
		"redirect":  ctx.Redirect,
		"serveFile": ctx.ServeFile,
		"flush":     ctx.Flush,
		"error":     ctx.Error,
	})
	return methods.Method
}

func (ctx *Context) SetStatus(L *lua.LState) int {
	code, err := L.CheckInt(1), L.OptString(2, "")
	if ctx.Hijacked || ctx.TimedOut {
		return 0
	}
	if util.CheckStatusCode(code) {
		ctx.StatusCode = code
		ctx.StatusText = http.StatusText(code)
		if err != "" {
			ctx.err = errors.New(err)
		} else {
			ctx.err = nil
		}
		ctx.w.WriteHeader(code)
	}
	return 0
}

func (ctx *Context) setStatus(code int, err error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.StatusCode = code
	ctx.StatusText = http.StatusText(code)
	ctx.err = err
}

func (ctx *Context) SetHeader(L *lua.LState) int {
	ctx.w.Header().Set(L.CheckString(1), L.CheckString(2))
	return 0
}

func (ctx *Context) DelHeader(L *lua.LState) int {
	ctx.w.Header().Del(L.CheckString(1))
	return 0
}

func (ctx *Context) Write(L *lua.LState) int {
	body := L.CheckString(1)
	var err error
	var size int
	if ctx.Hijacked || ctx.TimedOut {
		err = http.ErrHijacked
	} else {
		if ctx.StatusCode == 0 {
			ctx.setStatus(http.StatusOK, nil)
		}
		size, err = ctx.w.Write([]byte(body))
	}
	if err != nil {
		return util.NilError(L, err)
	}
	ctx.Size += size
	return util.Push(L, lua.LNumber(ctx.Size))
}

func (ctx *Context) ServeFile(L *lua.LState) int {
	root := L.CheckString(1)
	opts := L.OptTable(2, L.NewTable())
	if statusCode, err := serveFile(L, root, opts, ctx); err != nil {
		ctx.error(statusCode, err)
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) Redirect(L *lua.LState) int {
	url := L.CheckString(1)
	StatusCode := L.OptInt(2, http.StatusPermanentRedirect)
	if StatusCode < 300 || StatusCode > 308 {
		return util.Error(L, errors.New("invalid redirect status code"))
	}
	ctx.setStatus(StatusCode, nil)
	http.Redirect(ctx.w, ctx.r, url, StatusCode)
	return 0
}

func (ctx *Context) Flush(L *lua.LState) int {
	flusher, ok := ctx.w.(http.Flusher)
	if ok {
		flusher.Flush()
	}
	L.Push(lua.LBool(ok))
	return 1
}

func (ctx *Context) Error(L *lua.LState) int {
	statusCode := L.CheckInt(1)
	statusText := L.OptString(2, http.StatusText(statusCode))
	if !util.CheckStatusCode(statusCode) {
		return util.Error(L, errors.New("invalid redirect status code"))
	}
	ctx.error(statusCode, errors.New(statusText))
	return 0
}

func (ctx *Context) error(statusCode int, err error) {

	ctx.setStatus(statusCode, err)
	ctx.w.WriteHeader(statusCode)

	var errStr string
	if err != nil {
		errStr = err.Error()
	}

	data := &errorData{
		StatusCode: statusCode,
		StatusText: ctx.StatusText,
		Error:      errStr,
	}

	var tpl *template.Template
	var tplErr error

	if ctx.ErrorTemplate == "" {
		tpl, tplErr = util.ParseTemplateString(errorTemplate, "LUG_TPL_ERRORPAGE")
	} else {
		tpl, tplErr = util.ParseTemplateFiles(ctx.ErrorTemplate)
	}

	if tplErr == nil {
		tplErr = tpl.Execute(ctx.w, data)
	}

	if tplErr != nil {
		http.Error(ctx.w, ctx.StatusText, statusCode)
	}

	ctx.err = nil

}

func (ctx *Context) SetCookie(L *lua.LState) int {
	opts := L.CheckTable(1)
	cookie := &http.Cookie{}
	opts.ForEach(func(key, v lua.LValue) {
		k := key.String()
		switch k {

		case `name`:
			if val, ok := util.CheckString(L, k, v); ok {
				cookie.Name = val
			}

		case `value`:
			if val, ok := util.CheckString(L, k, v); ok {
				cookie.Value = val
			}

		case `path`:
			if val, ok := util.CheckString(L, k, v); ok {
				cookie.Path = val
			}

		case `domain`:
			if val, ok := util.CheckString(L, k, v); ok {
				cookie.Domain = val
			}

		case `expires`:
			if val, ok := util.CheckTime(L, k, v); ok {
				cookie.Expires = val
			}

		case `maxAge`:
			if val, ok := util.CheckInt(L, k, v); ok {
				cookie.MaxAge = val
			}

		case `secure`:
			if val, ok := util.CheckBool(L, k, v); ok {
				cookie.Secure = val
			}

		case `httpOnly`:
			if val, ok := util.CheckBool(L, k, v); ok {
				cookie.HttpOnly = val
			}

		case `sameSite`:
			var SameSite http.SameSite
			switch value := v.(type) {
			case lua.LNumber:
				SameSite = http.SameSite(int(value))
			case lua.LString:
				switch value.String() {
				case `lax`:
					SameSite = http.SameSiteLaxMode
				case `strict`:
					SameSite = http.SameSiteStrictMode
				case `none`:
					SameSite = http.SameSiteNoneMode
				default:
					SameSite = http.SameSiteDefaultMode
				}
			}
			cookie.SameSite = SameSite

		default:
			L.ArgError(1, "unknown cookie field: "+k)

		}
	})

	http.SetCookie(ctx.w, cookie)
	return 0
}
