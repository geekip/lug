package http

import (
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Request struct {
	Request    *http.Request
	Method     lua.LString
	Scheme     lua.LString
	Host       lua.LString
	Proto      lua.LString
	Path       lua.LString
	Referer    lua.LString
	UserAgent  lua.LString
	RawPath    lua.LString
	RawQuery   lua.LString
	RequestUri lua.LString
	RemoteAddr lua.LString
	Params     *lua.LTable
	Data       *lua.LTable
	StatusCode int
	StatusText string
	Size       int
	body       []byte
	bodyErr    error
}

var requestPool = sync.Pool{
	New: func() interface{} { return &Request{} },
}

func newRequest(r *http.Request) *Request {
	req := requestPool.Get().(*Request)
	req.Request = r
	req.Method = lua.LString(r.Method)
	req.Scheme = lua.LString(req.getScheme())
	req.Host = lua.LString(r.Host)
	req.Proto = lua.LString(r.Proto)
	req.Path = lua.LString(r.URL.Path)
	req.Referer = lua.LString(r.Referer())
	req.UserAgent = lua.LString(r.UserAgent())
	req.RawPath = lua.LString(r.URL.RawPath)
	req.RawQuery = lua.LString(r.URL.RawQuery)
	req.RequestUri = lua.LString(r.RequestURI)
	req.RemoteAddr = lua.LString(r.RemoteAddr)
	req.Params = &lua.LTable{}
	req.Data = &lua.LTable{}
	req.StatusCode = http.StatusOK
	req.StatusText = http.StatusText(http.StatusOK)
	req.Size = 0
	req.body = nil
	req.bodyErr = nil
	return req
}

func (req *Request) release() {
	req.Request = nil
	req.Method = ""
	req.Scheme = ""
	req.Host = ""
	req.Proto = ""
	req.Path = ""
	req.Referer = ""
	req.UserAgent = ""
	req.RawPath = ""
	req.RawQuery = ""
	req.RequestUri = ""
	req.RemoteAddr = ""
	req.Params = nil
	req.Data = nil
	req.StatusCode = 0
	req.StatusText = ""
	req.Size = 0
	req.body = nil
	req.bodyErr = nil
	requestPool.Put(req)
}

func (req *Request) getMethods(L *lua.LState) *lua.LTable {
	methods := util.NewModule(L, util.Methods{
		"params":       req.Params,
		"method":       req.Method,
		"scheme":       req.Scheme,
		"host":         req.Host,
		"proto":        req.Proto,
		"path":         req.Path,
		"referer":      req.Referer,
		"userAgent":    req.UserAgent,
		"rawPath":      req.RawPath,
		"rawQuery":     req.RawQuery,
		"requestUri":   req.RequestUri,
		"remoteAddr":   req.RemoteAddr,
		"getQuery":     req.GetQuery,
		"getHeader":    req.GetHeader,
		"getPath":      req.GetPath,
		"getCookie":    req.GetCookie,
		"getBody":      req.GetBody,
		"getClientIp":  req.GetClientIp,
		"basicAuth":    req.BasicAuth,
		"postForm":     req.PostForm,
		"getData":      req.Get,
		"setData":      req.Set,
		"getPathValue": req.GetPathValue,
		"setPathValue": req.SetPathValue,
	})
	return methods.Method
}

func (req *Request) GetHeader(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(req.Request.Header.Get(key)))
	return 1
}

func (req *Request) GetPath(L *lua.LState) int {
	L.Push(lua.LString(req.Request.URL.Path))
	return 1
}

func (req *Request) GetPathValue(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(req.Request.PathValue(key)))
	return 1
}

func (req *Request) SetPathValue(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckString(2)
	req.Request.SetPathValue(key, val)
	return 0
}

func (ctx *Request) GetQuery(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(ctx.Request.URL.Query().Get(key)))
	return 1
}

func (req *Request) GetBody(L *lua.LState) int {
	if req.body == nil && req.bodyErr == nil {
		req.body, req.bodyErr = io.ReadAll(req.Request.Body)
	}
	if req.bodyErr != nil {
		return util.NilError(L, req.bodyErr)
	}
	return util.Push(L, lua.LString(req.body))
}

func (req *Request) BasicAuth(L *lua.LState) int {
	u, p := L.CheckString(1), L.CheckString(2)
	if user, pass, ok := req.Request.BasicAuth(); !ok || user != u || pass != p {
		L.Push(lua.LFalse)
	} else {
		L.Push(lua.LTrue)
	}
	return 1
}

func (req *Request) PostForm(L *lua.LState) int {
	if err := req.Request.ParseForm(); err != nil {
		return util.NilError(L, err)
	}
	lform := L.NewTable()
	for key, values := range req.Request.PostForm {
		if len(values) > 0 {
			lform.RawSetString(key, lua.LString(values[0]))
		}
	}
	L.Push(lform)
	return 1
}

func (req *Request) GetClientIp(L *lua.LState) int {
	var cip string
	if ip := req.Request.Header.Get("X-Forwarded-For"); ip != "" {
		if ips := strings.Split(ip, ","); len(ips) > 0 {
			cip = strings.TrimSpace(ips[0])
		}
	} else if ip := req.Request.Header.Get("X-Real-IP"); ip != "" {
		cip = ip
	} else {
		host, _, err := net.SplitHostPort(req.Request.RemoteAddr)
		if err == nil {
			cip = host
		} else {
			cip = req.Request.RemoteAddr
		}
	}
	L.Push(lua.LString(cip))
	return 1
}

func (req *Request) GetCookie(L *lua.LState) int {
	key := L.CheckString(1)
	cookie, err := req.Request.Cookie(key)
	if err != nil {
		return util.NilError(L, err)
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

func (req *Request) Set(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckAny(2)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	req.Data.RawSetString(key, val)
	return 0
}

func (req *Request) Get(L *lua.LState) int {
	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	L.Push(req.Data.RawGetString(key))
	return 1
}

func (req *Request) getScheme() string {
	// Can't use `r.Request.URL.Scheme`
	// See: https://groups.google.com/forum/#!topic/golang-nuts/pMUkBlQBDF0
	header := req.Request.Header
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
