package http

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"text/template"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

var bufPool = sync.Pool{
	New: func() interface{} { return &bytes.Buffer{} },
}

// sets the HTTP status code of the response.
func (ctx *Context) SetStatus(L *lua.LState) int {
	if err := ctx.setStatus(L.CheckInt(1)); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) setStatus(statusCode int) error {

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.written {
		return errors.New("http: superfluous response.WriteHeader")
	}
	if err := ctx.checkHijacked(); err != nil {
		return err
	}
	if !util.CheckStatusCode(statusCode) {
		return fmt.Errorf("invalid status code: %d", statusCode)
	}

	ctx.statusCode = statusCode
	ctx.statusText = http.StatusText(statusCode)

	ctx.response.WriteHeader(statusCode)
	ctx.written = true
	return nil
}

func (ctx *Context) SetHeader(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckString(2)

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.response.Header().Set(key, val)
	return 0
}

func (ctx *Context) DelHeader(L *lua.LState) int {
	key := L.CheckString(1)

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.response.Header().Del(key)
	return 0
}

func (ctx *Context) Write(L *lua.LState) int {
	if err := ctx.checkHijacked(); err != nil {
		return util.Error(L, err)
	}
	body := L.CheckString(1)

	buf := bufPool.Get().(*bytes.Buffer)
	defer bufPool.Put(buf)

	buf.Reset()
	buf.WriteString(body)

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	size, err := ctx.response.Write(buf.Bytes())
	if err != nil {
		return util.NilError(L, err)
	}

	ctx.written = true
	ctx.size += size

	return util.Push(L, lua.LNumber(ctx.size))
}

func (ctx *Context) ServeFile(L *lua.LState) int {
	if err := ctx.checkHijacked(); err != nil {
		return util.Error(L, err)
	}
	root := L.CheckString(1)
	opts := L.OptTable(2, L.NewTable())
	if statusCode, err := serveFile(L, root, opts, ctx); err != nil {
		ctx.error(statusCode, err)
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) Redirect(L *lua.LState) int {
	if err := ctx.checkHijacked(); err != nil {
		return util.Error(L, err)
	}

	url := L.CheckString(1)
	statusCode := L.OptInt(2, http.StatusPermanentRedirect)
	// Validate the redirect status code.
	// http.StatusMultipleChoices 300,http.StatusPermanentRedirect 308
	if statusCode < 300 || statusCode > 308 {
		return util.Error(L, errors.New("invalid redirect status code (300-308)"))
	}

	if ctx.written {
		return util.Error(L, errors.New("http: superfluous response.WriteHeader"))
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.statusCode = statusCode
	ctx.statusText = http.StatusText(statusCode)
	ctx.written = true

	http.Redirect(ctx.response, ctx.request, url, statusCode)
	return 0
}

func (ctx *Context) Flush(L *lua.LState) int {
	if err := ctx.checkHijacked(); err != nil {
		return util.Error(L, err)
	}

	flusher, ok := ctx.response.(http.Flusher)
	if ok {
		flusher.Flush()
	}
	return util.Push(L, lua.LBool(ok))
}

func (ctx *Context) Error(L *lua.LState) int {
	statusCode := L.CheckInt(1)

	var err error = nil
	if L.GetTop() > 1 {
		err = errors.New(L.CheckString(2))
	}

	if e := ctx.error(statusCode, err); e != nil {
		return util.Error(L, e)
	}
	return 0
}

func (ctx *Context) error(statusCode int, err error) error {
	if e := ctx.setStatus(statusCode); e != nil {
		return e
	}

	if statusCode < 400 || statusCode > 599 {
		return errors.New("http: invalid error status code (range 400–599)")
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.statusError = err

	var tpl *template.Template
	var tplErr error

	if ctx.errorTemplate == "" {
		tpl, tplErr = util.ParseTemplateString(errorTemplate, "LUG_TPL_ERRORPAGE")
	} else {
		tpl, tplErr = util.ParseTemplateFiles(ctx.errorTemplate)
	}

	if tplErr != nil {
		http.Error(ctx.response, ctx.statusText, statusCode)
		return tplErr
	}

	status := &Status{
		StatusCode:  statusCode,
		StatusText:  ctx.statusText,
		StatusError: err,
	}

	if execErr := tpl.Execute(ctx.response, status); execErr != nil {
		http.Error(ctx.response, ctx.statusText, statusCode)
		return execErr
	}

	return nil
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
			sameSite, err := ctx.parseSameSite(v)
			if err != nil {
				L.ArgError(1, err.Error())
			}
			cookie.SameSite = sameSite

		default:
			L.ArgError(1, "unknown cookie field: "+k)
		}
	})

	http.SetCookie(ctx.response, cookie)
	return 0
}

func (ctx *Context) parseSameSite(v lua.LValue) (http.SameSite, error) {
	switch value := v.(type) {
	case lua.LNumber:
		code := int(value)
		if code < 0 || code > 3 {
			return 0, errors.New("invalid error sameSite (range 0-3)")
		}
		return http.SameSite(code), nil
	case lua.LString:
		switch value.String() {
		case "lax":
			return http.SameSiteLaxMode, nil
		case "strict":
			return http.SameSiteStrictMode, nil
		case "none":
			return http.SameSiteNoneMode, nil
		default:
			return http.SameSiteDefaultMode, nil
		}
	default:
		err := errors.New("sameSite must be number or string")
		return http.SameSiteDefaultMode, err
	}
}

func (ctx *Context) DelCookie(L *lua.LState) int {
	cookie, err := ctx.request.Cookie(L.CheckString(1))
	if err != nil {
		return util.Error(L, err)
	}
	cookie.MaxAge = -1
	http.SetCookie(ctx.response, cookie)
	return 0
}

func (ctx *Context) checkHijacked() error {
	switch {
	case ctx.hijacked:
		return errors.New("http: response already hijacked")
	case ctx.timedOut:
		return errors.New("http: response processing timeout")
	default:
		return nil
	}
}

func (ctx *Context) Hijack(L *lua.LState) int {

	if err := ctx.checkHijacked(); err != nil {
		return util.NilError(L, err)
	}

	if ctx.written {
		err := errors.New("http: superfluous response.WriteHeader")
		return util.NilError(L, err)
	}

	hiJacker, ok := ctx.response.(http.Hijacker)
	if !ok {
		err := errors.New("connection doesn't support hijacking")
		return util.NilError(L, err)
	}

	conn, bufrw, err := hiJacker.Hijack()
	if err != nil {
		return util.NilError(L, err)
	}

	ctx.mu.Lock()
	ctx.hijacked = true
	ctx.conn = conn
	ctx.bufrw = bufrw
	ctx.mu.Unlock()

	hj := util.SetMethods(L, util.Methods{
		"read":  ctx.HjRead,
		"write": ctx.HjWrite,
		"close": ctx.HjClose,
	})

	return util.Push(L, hj)
}

func (ctx *Context) HjRead(L *lua.LState) int {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if !ctx.hijacked || ctx.bufrw == nil {
		return util.NilError(L, errors.New("connection not hijacked"))
	}

	n := L.OptInt(1, 1024)
	buf := make([]byte, n)

	size, err := ctx.bufrw.Read(buf)
	if err != nil {
		return util.NilError(L, err)
	}

	return util.Push(L, lua.LString(buf[:size]))
}

func (ctx *Context) HjWrite(L *lua.LState) int {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if !ctx.hijacked || ctx.bufrw == nil {
		return util.NilError(L, errors.New("connection not hijacked"))
	}

	data := L.CheckString(1)
	size, err := ctx.bufrw.WriteString(data)
	if err != nil {
		return util.NilError(L, err)
	}
	if err := ctx.bufrw.Flush(); err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LNumber(size))
}

func (ctx *Context) HjClose(L *lua.LState) int {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.conn != nil {
		ctx.conn.Close()
		ctx.conn = nil
		ctx.bufrw = nil
		// ctx.Hijacked = false
	}
	return 0
}
