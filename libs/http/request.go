package http

import (
	"io"
	"net"
	"net/http"
	"strings"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

func (req *Context) Referer(L *lua.LState) int {
	return util.Push(L, lua.LString(req.request.Referer()))
}

func (req *Context) UserAgent(L *lua.LState) int {
	return util.Push(L, lua.LString(req.request.UserAgent()))
}

func (ctx *Context) Set(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckAny(2)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	ctx.data.RawSetString(key, val)
	return 0
}

func (ctx *Context) Get(L *lua.LState) int {
	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	return util.Push(L, ctx.data.RawGetString(key))
}

func (req *Context) GetHeader(L *lua.LState) int {
	return util.Push(L, lua.LString(req.request.Header.Get(L.CheckString(1))))
}

func (ctx *Context) GetPath(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.request.URL.Path))
}

func (ctx *Context) SetPath(L *lua.LState) int {
	ctx.request.URL.Path = L.CheckString(1)
	return 0
}

func (ctx *Context) stripPrefix(prefix string) {
	if prefix == "" {
		return
	}
	path := ctx.request.URL.Path
	if strings.HasPrefix(path, prefix) {
		newPath := strings.TrimPrefix(path, prefix)
		if newPath == "" {
			newPath = "/"
		}
		ctx.request.URL.Path = newPath
		ctx.stripPath = newPath
	}
}

func (ctx *Context) GetQuery(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.request.URL.Query().Get(L.CheckString(1))))
}

func (ctx *Context) BasicAuth(L *lua.LState) int {
	u, p := L.CheckString(1), L.CheckString(2)
	user, pass, ok := ctx.request.BasicAuth()
	if !ok || user != u || pass != p {
		return util.Push(L, lua.LFalse)
	}
	return util.Push(L, lua.LTrue)
}

func (ctx *Context) GetBody(L *lua.LState) int {
	body, err := io.ReadAll(ctx.request.Body)
	defer ctx.request.Body.Close()
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(body))
}

func (ctx *Context) PostForm(L *lua.LState) int {
	r := ctx.request
	if err := r.ParseForm(); err != nil {
		return util.NilError(L, err)
	}
	lform := L.NewTable()
	for key, values := range r.PostForm {
		if len(values) > 0 {
			lform.RawSetString(key, lua.LString(values[0]))
		}
	}
	return util.Push(L, lform)
}

func (ctx *Context) RemoteIP(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.remoteIP()))
}

func (ctx *Context) remoteIP() string {
	if ip := ctx.request.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := ctx.request.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(ctx.request.RemoteAddr); err == nil {
		return host
	}
	return ctx.request.RemoteAddr
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
	cookie, err := ctx.request.Cookie(L.CheckString(1))
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, ctx.transformCookie(L, cookie))
}

func (ctx *Context) GetCookies(L *lua.LState) int {
	cookies := ctx.request.Cookies()
	lcookies := L.NewTable()
	for _, v := range cookies {
		lcookies.RawSetString(v.Name, ctx.transformCookie(L, v))
	}
	return util.Push(L, lcookies)
}

func (ctx *Context) GetScheme(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.getScheme()))
}

func (ctx *Context) getScheme() string {
	// Can't use `r.Request.URL.Scheme`
	// See: https://groups.google.com/forum/#!topic/golang-nuts/pMUkBlQBDF0
	header := ctx.request.Header
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
