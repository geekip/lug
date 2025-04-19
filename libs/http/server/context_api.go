package server

import (
	"errors"
	"io/fs"
	"lug/util"
	"net/http"
	"path/filepath"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

func (ctx *Context) LuaContext(L *lua.LState) *lua.LTable {
	r := ctx.request

	api := util.Methods{
		"params":         ctx.getParams(),
		"method":         lua.LString(r.Method),
		"host":           lua.LString(r.Host),
		"proto":          lua.LString(r.Proto),
		"path":           lua.LString(r.URL.Path),
		"rawPath":        lua.LString(r.URL.RawPath),
		"rawQuery":       lua.LString(r.URL.RawQuery),
		"requestUri":     lua.LString(r.RequestURI),
		"remoteAddr":     lua.LString(r.RemoteAddr),
		"disableCache":   ctx.disableCache,
		"remoteIP":       ctx.remoteIP,
		"referer":        ctx.referer,
		"query":          ctx.getQuery,
		"port":           ctx.getPort,
		"userAgent":      ctx.userAgent,
		"basicAuth":      ctx.basicAuth,
		"postForm":       ctx.postForm,
		"body":           ctx.getBody,
		"scheme":         ctx.getScheme,
		"getData":        ctx.getData,
		"setData":        ctx.setData,
		"delData":        ctx.delData,
		"getPath":        ctx.getPath,
		"setPath":        ctx.setPath,
		"setStatus":      ctx.setStatus,
		"getHeader":      ctx.getHeader,
		"setHeader":      ctx.setHeader,
		"delHeader":      ctx.delHeader,
		"getCookie":      ctx.getCookie,
		"getCookies":     ctx.getCookies,
		"setCookie":      ctx.setCookie,
		"delCookie":      ctx.delCookie,
		"since":          ctx.since,
		"route":          ctx.getRoute,
		"cors":           ctx.cors,
		"write":          ctx.write,
		"flush":          ctx.flush,
		"redirect":       ctx.redirect,
		"hijack":         ctx.hijack,
		"serveFile":      ctx.serveFile,
		"uploadFile":     ctx.uploadFile,
		"attachmentFile": ctx.attachmentFile,
		"error":          ctx.error,
	}
	return util.SetMethods(L, api)
}

func (ctx *Context) getParams() *lua.LTable {
	lparams := &lua.LTable{}
	for k, v := range ctx.params {
		lparams.RawSetString(k, lua.LString(v))
	}
	return lparams
}

func (ctx *Context) since(L *lua.LState) int {
	return util.Push(L, lua.LNumber(ctx.Since()))
}

func (ctx *Context) referer(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.request.Referer()))
}

func (req *Context) userAgent(L *lua.LState) int {
	return util.Push(L, lua.LString(req.request.UserAgent()))
}

func (ctx *Context) setData(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckAny(2)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	ctx.SetData(key, val)
	return 0
}

func (ctx *Context) getData(L *lua.LState) int {
	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	data, ok := ctx.GetData(key)
	return util.Push(L, util.ToLuaValue(data), lua.LBool(ok))
}

func (ctx *Context) delData(L *lua.LState) int {
	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	ctx.DelData(key)
	return 0
}

func (ctx *Context) getHeader(L *lua.LState) int {
	header := ctx.GetHeader(L.CheckString(1))
	return util.Push(L, lua.LString(header))
}

func (ctx *Context) getPath(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.GetPath()))
}

func (ctx *Context) setPath(L *lua.LState) int {
	ctx.request.URL.Path = L.CheckString(1)
	return 0
}

func (ctx *Context) getRoute(L *lua.LState) int {
	route := util.SetMethods(L, util.Methods{
		"host":    lua.LString(ctx.route.host),
		"pattern": lua.LString(ctx.route.pattern),
		"methods": ctx.route.methods,
	})
	return util.Push(L, route)
}

func (ctx *Context) getQuery(L *lua.LState) int {
	query := ctx.GetQuery(L.CheckString(1))
	return util.Push(L, lua.LString(query))
}

func (ctx *Context) getPort(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.GetPort()))
}

func (ctx *Context) basicAuth(L *lua.LState) int {
	user, passwd := L.CheckString(1), L.CheckString(2)
	return util.Push(L, lua.LBool(ctx.BasicAuth(user, passwd)))
}

func (ctx *Context) getBody(L *lua.LState) int {
	body, err := ctx.GetBody()
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(body))
}

func (ctx *Context) postForm(L *lua.LState) int {
	form, err := ctx.PostForm()
	if err != nil {
		return util.NilError(L, err)
	}
	lform := L.NewTable()
	for key, values := range form {
		lform.RawSetString(key, lua.LString(values))
	}
	return util.Push(L, lform)
}

func (ctx *Context) remoteIP(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.RemoteIP()))
}

func (ctx *Context) getScheme(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.GetScheme()))
}

func (ctx *Context) setStatus(L *lua.LState) int {
	if err := ctx.SetStatus(L.CheckInt(1)); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) setHeader(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckString(2)
	ctx.SetHeader(key, val)
	return 0
}

func (ctx *Context) delHeader(L *lua.LState) int {
	ctx.DelHeader(L.CheckString(1))
	return 0
}

func (ctx *Context) disableCache(L *lua.LState) int {
	ctx.DisableCache()
	return 0
}

func (ctx *Context) write(L *lua.LState) int {
	body := []byte(L.CheckString(1))
	size, err := ctx.Write(body)
	if err != nil {
		return util.Error(L, err)
	}
	return util.Push(L, lua.LNumber(size))
}

func (ctx *Context) redirect(L *lua.LState) int {
	url := L.CheckString(1)
	statusCode := L.OptInt(2, http.StatusPermanentRedirect)
	if err := ctx.Redirect(url, statusCode); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) flush(L *lua.LState) int {
	if err := ctx.Flush(); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) error(L *lua.LState) int {
	statusCode, statusError := L.CheckInt(1), L.CheckString(2)
	var err error = nil
	if L.GetTop() > 1 {
		err = errors.New(statusError)
	}
	if e := ctx.Error(statusCode, err); e != nil {
		return util.Error(L, e)
	}
	return 0
}

func (ctx *Context) getCookie(L *lua.LState) int {
	cookie, err := ctx.GetCookie(L.CheckString(1))
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, transformCookie(L, cookie))
}

func (ctx *Context) getCookies(L *lua.LState) int {
	cookies := ctx.GetCookies()
	lcookies := L.NewTable()
	for _, v := range cookies {
		lcookies.RawSetString(v.Name, transformCookie(L, v))
	}
	return util.Push(L, lcookies)
}

func (ctx *Context) setCookie(L *lua.LState) int {
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
			sameSite, err := parseSameSite(v)
			if err != nil {
				L.ArgError(1, err.Error())
			}
			cookie.SameSite = sameSite

		default:
			L.ArgError(1, "unknown cookie field: "+k)
		}
	})

	ctx.SetCookie(cookie)
	return 0
}

func (ctx *Context) delCookie(L *lua.LState) int {
	err := ctx.DelCookie(L.CheckString(1))
	if err != nil {
		return util.Error(L, err)
	}
	return 0
}

func parseSameSite(v lua.LValue) (http.SameSite, error) {
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

func transformCookie(L *lua.LState, cookie *http.Cookie) *lua.LTable {
	unparsedTable := L.NewTable()
	for _, u := range cookie.Unparsed {
		unparsedTable.Append(lua.LString(u))
	}
	lCookie := map[string]lua.LValue{
		"name":     lua.LString(cookie.Name),
		"value":    lua.LString(cookie.Value),
		"path":     lua.LString(cookie.Path),
		"domain":   lua.LString(cookie.Domain),
		"expires":  lua.LNumber(cookie.Expires.Unix()),
		"maxAge":   lua.LNumber(cookie.MaxAge),
		"secure":   lua.LBool(cookie.Secure),
		"httpOnly": lua.LBool(cookie.HttpOnly),
		"sameSite": lua.LNumber(cookie.SameSite),
		"raw":      lua.LString(cookie.Raw),
		"unparsed": unparsedTable,
	}
	lcookies := L.NewTable()
	for k, v := range lCookie {
		lcookies.RawSetString(k, v)
	}
	return lcookies
}

func (ctx *Context) hijack(L *lua.LState) int {
	if err := ctx.Hijack(); err != nil {
		return util.NilError(L, err)
	}
	hj := util.SetMethods(L, util.Methods{
		"read":  ctx.readHijack,
		"write": ctx.writeHijack,
		"close": ctx.closeHijack,
	})
	return util.Push(L, hj)
}

func (ctx *Context) readHijack(L *lua.LState) int {
	size := L.OptInt(1, 1024)
	body, err := ctx.ReadHijack(size)
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(body))
}

func (ctx *Context) writeHijack(L *lua.LState) int {
	body := L.CheckString(1)
	size, err := ctx.WriteHijack([]byte(body))
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LNumber(size))
}

func (ctx *Context) closeHijack(L *lua.LState) int {
	ctx.CloseHijack()
	return 0
}

func (ctx *Context) serveFile(L *lua.LState) int {

	filePath := L.CheckString(1)
	lopt := L.OptTable(2, L.NewTable())
	opt := &defaultServeFileOpts

	lopt.ForEach(func(k, v lua.LValue) {
		key := k.String()
		switch key {
		case "ignoreBase":
			if val, ok := util.CheckBool(L, key, v); ok {
				opt.ignoreBase = val
			}
		case "autoIndex":
			if val, ok := util.CheckBool(L, key, v); ok {
				opt.autoIndex = val
			}
		case "index":
			if val, ok := util.CheckTable(L, key, v); ok {
				opt.index = val
			}
		case "prettyIndex":
			if val, ok := util.CheckBool(L, key, v); ok {
				opt.prettyIndex = val
			}
		}
	})
	status := ctx.ServeFile(filePath, opt)
	if status.StatusError != nil {
		return util.Error(L, status.StatusError)
	}
	return 0
}

func (ctx *Context) uploadFile(L *lua.LState) int {
	fieldName, dst := L.CheckString(1), L.CheckString(2)
	mode := fs.FileMode(L.OptInt(3, 0o750))
	if err := ctx.UploadFile(fieldName, dst, mode); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) attachmentFile(L *lua.LState) int {
	filePath := L.CheckString(1)
	fileName := L.OptString(2, filepath.Base(filePath))
	status := ctx.AttachmentFile(filePath, fileName)
	if status.StatusError != nil {
		return util.Error(L, status.StatusError)
	}
	return 0
}

func (ctx *Context) cors(L *lua.LState) int {
	opt := L.OptTable(1, L.NewTable())
	cfg := getCorsConfig(L, opt, &defaultCorsConfig)
	ctx.Cors(*cfg)
	return 0
}

func invokeAllowOriginFunc(L *lua.LState, fn *lua.LFunction) func(string) bool {
	return func(origin string) bool {
		if err := L.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, lua.LString(origin)); err != nil {
			L.RaiseError("originFunc error: %v", err)
			return false
		}

		ret := L.Get(-1)
		L.Pop(1)

		allowed, ok := ret.(lua.LBool)
		if !ok {
			L.RaiseError("originFunc must return boolean")
			return false
		}

		return allowed == lua.LTrue
	}

}

func getCorsConfig(L *lua.LState, opt *lua.LTable, defaultCfg *corsConfig) *corsConfig {
	cfg := *defaultCfg
	opt.ForEach(func(k lua.LValue, v lua.LValue) {
		key := k.String()
		switch key {
		case "origins":
			if val, ok := util.CheckTable(L, key, v); ok {
				if len(val) > 0 {
					cfg.origins = val
				}
			}
		case "originFunc":
			if val, ok := util.CheckFunction(L, key, v); ok {
				cfg.originFunc = invokeAllowOriginFunc(L, val)
			}
		case "methods":
			if val, ok := util.CheckTable(L, key, v); ok {
				if len(val) > 0 {
					cfg.methods = val
					cfg.hasCustomAllowMethods = true
				}
			}
		case "allowedHeaders":
			if val, ok := util.CheckTable(L, key, v); ok {
				cfg.allowedHeaders = val
			}
		case "credentials":
			if val, ok := util.CheckBool(L, key, v); ok {
				cfg.credentials = val
			}
		case "allowWildcard":
			if val, ok := util.CheckBool(L, key, v); ok {
				cfg.allowWildcard = val
			}
		case "exposeHeaders":
			if val, ok := util.CheckTable(L, key, v); ok {
				cfg.exposeHeaders = val
			}
		case "maxAge":
			if val, ok := util.CheckDuration(L, key, v); ok {
				cfg.maxAge = val
			}
		default:
			L.ArgError(1, "unknown CORS field: "+key)
		}
	})
	return &cfg
}
