package http

import (
	"io"
	"net"
	"net/http"
	"strings"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

func (ctx *Context) getRequestLuaApi(L *lua.LState) *lua.LTable {
	methods := util.NewModule(L, util.Methods{
		"params":       ctx.Params,
		"method":       lua.LString(ctx.r.Method),
		"host":         lua.LString(ctx.r.Host),
		"proto":        lua.LString(ctx.r.Proto),
		"path":         lua.LString(ctx.r.URL.Path),
		"referer":      lua.LString(ctx.r.Referer()),
		"userAgent":    lua.LString(ctx.r.UserAgent()),
		"rawPath":      lua.LString(ctx.r.URL.RawPath),
		"rawQuery":     lua.LString(ctx.r.URL.RawQuery),
		"requestUri":   lua.LString(ctx.r.RequestURI),
		"remoteAddr":   lua.LString(ctx.r.RemoteAddr),
		"getQuery":     ctx.GetQuery,
		"getHeader":    ctx.GetHeader,
		"getPath":      ctx.GetPath,
		"getCookie":    ctx.GetCookie,
		"getCookies":   ctx.GetCookies,
		"getBody":      ctx.GetBody,
		"getClientIp":  ctx.GetClientIp,
		"basicAuth":    ctx.BasicAuth,
		"postForm":     ctx.PostForm,
		"getData":      ctx.Get,
		"setData":      ctx.Set,
		"getPathValue": ctx.GetPathValue,
		"setPathValue": ctx.SetPathValue,
		"getScheme":    ctx.GetScheme,
	})
	return methods.Method
}

func (req *Context) GetHeader(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(req.r.Header.Get(key)))
	return 1
}

func (ctx *Context) GetPath(L *lua.LState) int {
	L.Push(lua.LString(ctx.r.URL.Path))
	return 1
}

func (ctx *Context) GetPathValue(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(ctx.r.PathValue(key)))
	return 1
}

func (ctx *Context) SetPathValue(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckString(2)
	ctx.r.SetPathValue(key, val)
	return 0
}

func (ctx *Context) GetQuery(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(ctx.r.URL.Query().Get(key)))
	return 1
}

func (ctx *Context) BasicAuth(L *lua.LState) int {
	u, p := L.CheckString(1), L.CheckString(2)
	if user, pass, ok := ctx.r.BasicAuth(); !ok || user != u || pass != p {
		L.Push(lua.LFalse)
	} else {
		L.Push(lua.LTrue)
	}
	return 1
}

func (ctx *Context) GetBody(L *lua.LState) int {
	body, err := io.ReadAll(ctx.r.Body)
	defer ctx.r.Body.Close()
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(body))
}

func (ctx *Context) PostForm(L *lua.LState) int {
	r := ctx.r
	if err := r.ParseForm(); err != nil {
		return util.NilError(L, err)
	}
	lform := L.NewTable()
	for key, values := range r.PostForm {
		if len(values) > 0 {
			lform.RawSetString(key, lua.LString(values[0]))
		}
	}
	L.Push(lform)
	return 1
}

func (ctx *Context) GetClientIp(L *lua.LState) int {
	cip := ctx.getClientIp()
	L.Push(lua.LString(cip))
	return 1
}

func (ctx *Context) getClientIp() string {
	var cip string
	if ip := ctx.r.Header.Get("X-Forwarded-For"); ip != "" {
		if ips := strings.Split(ip, ","); len(ips) > 0 {
			cip = strings.TrimSpace(ips[0])
		}
	} else if ip := ctx.r.Header.Get("X-Real-IP"); ip != "" {
		cip = ip
	} else {
		host, _, err := net.SplitHostPort(ctx.r.RemoteAddr)
		if err == nil {
			cip = host
		} else {
			cip = ctx.r.RemoteAddr
		}
	}
	return cip
}

func (ctx *Context) transformCookie(L *lua.LState, cookie *http.Cookie) *lua.LTable {
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

func (ctx *Context) GetCookie(L *lua.LState) int {
	key := L.CheckString(1)
	cookie, err := ctx.r.Cookie(key)
	if err != nil {
		return util.NilError(L, err)
	}
	L.Push(ctx.transformCookie(L, cookie))
	return 1
}

func (ctx *Context) GetCookies(L *lua.LState) int {
	cookies := ctx.r.Cookies()
	lcookies := L.NewTable()
	for _, v := range cookies {
		lcookies.RawSetString(v.Name, ctx.transformCookie(L, v))
	}
	L.Push(lcookies)
	return 1
}

func (ctx *Context) Set(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckAny(2)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	ctx.Data.RawSetString(key, val)
	return 0
}

func (ctx *Context) Get(L *lua.LState) int {
	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	L.Push(ctx.Data.RawGetString(key))
	return 1
}

func (ctx *Context) GetScheme(L *lua.LState) int {
	L.Push(lua.LString(ctx.getScheme()))
	return 1
}

func (ctx *Context) getScheme() string {
	// Can't use `r.Request.URL.Scheme`
	// See: https://groups.google.com/forum/#!topic/golang-nuts/pMUkBlQBDF0
	header := ctx.r.Header
	if scheme := header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	if scheme := header.Get("X-Forwarded-Protocol"); scheme != "" {
		return scheme
	}
	if ssl := header.Get("X-Forwarded-Ssl"); ssl == "on" {
		return "https"
	}
	if scheme := header.Get("X-Url-Scheme"); scheme != "" {
		return scheme
	}
	return "http"
}
