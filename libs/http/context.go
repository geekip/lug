package http

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Context struct {
	Request  *http.Request
	Response http.ResponseWriter
	Method   lua.LString
	Path     lua.LString
	Data     *lua.LTable
	Params   *lua.LTable
	Status   lua.LNumber
	Next     func() string
	mu       sync.RWMutex
	isEnd    bool
}

var contextPool = sync.Pool{
	New: func() interface{} {
		return &Context{
			Params: &lua.LTable{},
			Status: lua.LNumber(http.StatusOK),
		}
	},
}

func newContext(w http.ResponseWriter, r *http.Request) *Context {
	ctx := contextPool.Get().(*Context)
	ctx.Request = r
	ctx.Response = w
	ctx.Method = lua.LString(r.Method)
	ctx.Path = lua.LString(r.URL.Path)
	ctx.Data = &lua.LTable{}
	return ctx
}

func (ctx *Context) Release() {
	ctx.Request = nil
	ctx.Response = nil
	ctx.Method = lua.LString("")
	ctx.Path = lua.LString("")
	ctx.Data = &lua.LTable{}
	ctx.Params = &lua.LTable{}
	ctx.Status = lua.LNumber(http.StatusOK)
	ctx.Next = nil
	ctx.isEnd = false
	contextPool.Put(ctx)
}

func (ctx *Context) End() {
	ctx.isEnd = true
}

func (ctx *Context) Error(err string, code int) {
	http.Error(ctx.Response, err, code)
	ctx.End()
}

func (ctx *Context) error(L *lua.LState) int {
	topLen := L.GetTop()
	statusCode := http.StatusInternalServerError
	statusText := http.StatusText(statusCode)

	if topLen >= 1 {
		statusText = L.CheckString(1)
		if topLen >= 2 {
			statusCode = L.CheckInt(2)
		}
	}
	ctx.Error(statusText, statusCode)
	return 0
}

func (ctx *Context) getLuaApi(L *lua.LState) *lua.LTable {
	mod := util.NewModule(L, util.Methods{
		"method":      ctx.Method,
		"path":        ctx.Path,
		"params":      ctx.Params,
		"getData":     ctx.getData,
		"setData":     ctx.setData,
		"setStatus":   ctx.setStatus,
		"getQuery":    ctx.getQuery,
		"setHeader":   ctx.setHeader,
		"getHeader":   ctx.getHeader,
		"getCookie":   ctx.getCookie,
		"setCookie":   ctx.setCookie,
		"files":       ctx.files,
		"file":        ctx.file,
		"basicAuth":   ctx.basicAuth,
		"postForm":    ctx.postForm,
		"error":       ctx.error,
		"redirect":    ctx.redirect,
		"getClientIp": ctx.getClientIp,
		"next":        ctx.next,
	})
	return mod.Method
}

func (ctx *Context) next(L *lua.LState) int {
	var body string
	if ctx.Next != nil {
		body = ctx.Next()
	} else {
		body = ""
	}
	L.Push(lua.LString(body))
	return 1
}

func (ctx *Context) setData(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckAny(2)

	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
		return 0
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.Data.RawSetString(key, val)
	return 0
}

func (ctx *Context) getData(L *lua.LState) int {

	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
		return 0
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	value := ctx.Data.RawGetString(key)
	L.Push(value)
	return 1
}

func (ctx *Context) setStatus(L *lua.LState) int {
	code := L.CheckInt(1)
	if util.CheckStatusCode(code) {
		ctx.Status = lua.LNumber(code)
	}
	return 0
}

func (ctx *Context) getQuery(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(ctx.Request.URL.Query().Get(key)))
	return 1
}

func (ctx *Context) setHeader(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckString(2)
	if strings.TrimSpace(val) == "" {
		ctx.Response.Header().Del(key)
	} else {
		ctx.Response.Header().Set(key, val)
	}
	return 0
}

func (ctx *Context) getHeader(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(ctx.Request.Header.Get(key)))
	return 1
}

func (ctx *Context) setCookie(L *lua.LState) int {
	opts := L.CheckTable(1)
	cookie := &http.Cookie{}
	opts.ForEach(func(key, val lua.LValue) {
		k := key.String()
		switch k {
		case `Name`:
			if v, ok := val.(lua.LString); ok {
				cookie.Name = v.String()
			} else {
				L.ArgError(1, "Name must be string")
			}
		case `Value`:
			if v, ok := val.(lua.LString); ok {
				cookie.Value = v.String()
			} else {
				L.ArgError(1, "Value must be a string")
			}
		case `Path`:
			if v, ok := val.(lua.LString); ok {
				cookie.Path = v.String()
			} else {
				L.ArgError(1, "Path must be a string")
			}
		case `Domain`:
			if v, ok := val.(lua.LString); ok {
				cookie.Domain = v.String()
			} else {
				L.ArgError(1, "Domain must be a string")
			}
		case `Expires`:
			if v, ok := val.(lua.LString); ok {
				t, err := time.Parse(time.RFC3339, v.String())
				if err != nil {
					L.ArgError(1, "Invalid Expires format: "+err.Error())
				}
				cookie.Expires = t
			} else {
				L.ArgError(1, "Expires must be a string")
			}
		case `MaxAge`:
			if v, ok := val.(lua.LNumber); ok {
				cookie.MaxAge = int(v)
			} else {
				L.ArgError(1, "MaxAge must be a number")
			}
		case `Secure`:
			if v, ok := val.(lua.LBool); ok {
				cookie.Secure = bool(v)
			} else {
				L.ArgError(1, "Secure must be a boolean")
			}
		case `HttpOnly`:
			if v, ok := val.(lua.LBool); ok {
				cookie.HttpOnly = bool(v)
			} else {
				L.ArgError(1, "HttpOnly must be a boolean")
			}
		default:
			L.ArgError(1, "Unknown cookie field: "+k)
		}
	})

	http.SetCookie(ctx.Response, cookie)
	return 0
}

func (ctx *Context) getCookie(L *lua.LState) int {
	key := L.CheckString(1)
	cookie, err := ctx.Request.Cookie(key)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	unparsedTable := L.NewTable()
	for _, u := range cookie.Unparsed {
		unparsedTable.Append(lua.LString(u))
	}
	lCookie := map[string]lua.LValue{
		"Name":     lua.LString(cookie.Name),
		"Value":    lua.LString(cookie.Value),
		"Path":     lua.LString(cookie.Path),
		"Domain":   lua.LString(cookie.Domain),
		"Expires":  lua.LNumber(cookie.Expires.Unix()),
		"MaxAge":   lua.LNumber(cookie.MaxAge),
		"Secure":   lua.LBool(cookie.Secure),
		"HttpOnly": lua.LBool(cookie.HttpOnly),
		"SameSite": lua.LNumber(cookie.SameSite),
		"Raw":      lua.LString(cookie.Raw),
		"Unparsed": unparsedTable,
	}
	lcookies := L.NewTable()
	for k, v := range lCookie {
		lcookies.RawSetString(k, v)
	}
	L.Push(lcookies)
	return 1
}

func (ctx *Context) files(L *lua.LState) int {
	fs := http.FileServer(http.Dir(L.CheckString(1)))
	key := ctx.Params.RawGetString("*").String()
	basePath := strings.TrimSuffix(ctx.Path.String(), key)
	http.StripPrefix(basePath, fs).ServeHTTP(ctx.Response, ctx.Request)
	ctx.End()
	return 0
}

func (ctx *Context) file(L *lua.LState) int {
	http.ServeFile(ctx.Response, ctx.Request, L.CheckString(1))
	ctx.End()
	return 0
}

func (ctx *Context) basicAuth(L *lua.LState) int {
	u, p := L.CheckString(1), L.CheckString(2)
	if user, pass, ok := ctx.Request.BasicAuth(); !ok || user != u || pass != p {
		L.Push(lua.LFalse)
	} else {
		L.Push(lua.LTrue)
	}

	return 1
}

func (ctx *Context) postForm(L *lua.LState) int {
	if err := ctx.Request.ParseForm(); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	lform := L.NewTable()
	for key, values := range ctx.Request.PostForm {
		if len(values) > 0 {
			lform.RawSetString(key, lua.LString(values[0]))
		}
	}
	L.Push(lform)
	return 1
}

func (ctx *Context) redirect(L *lua.LState) int {
	url := L.CheckString(1)
	code := http.StatusMovedPermanently
	if L.GetTop() >= 2 {
		if c := L.CheckInt(2); util.CheckStatusCode(c) {
			code = c
		}
	}
	http.Redirect(ctx.Response, ctx.Request, url, code)
	ctx.End()
	return 0
}

func (ctx *Context) getClientIp(L *lua.LState) int {
	var cip string
	if ip := ctx.Request.Header.Get("X-Forwarded-For"); ip != "" {
		ips := strings.Split(ip, ",")
		if len(ips) > 0 {
			ip = strings.TrimSpace(ips[0])
		}
		cip = ip
	} else if ip := ctx.Request.Header.Get("X-Real-IP"); ip != "" {
		cip = ip
	} else {
		cip = strings.Split(ctx.Request.RemoteAddr, ":")[0]
	}
	L.Push(lua.LString(cip))
	return 1
}
