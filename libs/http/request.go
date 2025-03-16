package http

import (
	"io"
	"lug/util"
	"net/http"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type Request struct {
	http.ResponseWriter
	*http.Request
	method     lua.LString
	host       lua.LString
	proto      lua.LString
	path       lua.LString
	referer    lua.LString
	userAgent  lua.LString
	rawPath    lua.LString
	rawQuery   lua.LString
	requestUri lua.LString
	remoteAddr lua.LString
	params     *lua.LTable
	query      *lua.LTable
	headers    *lua.LTable
	Data       *lua.LTable
	mu         sync.RWMutex
	Next       func()
}

type ctxKey int

var paramCtxKey ctxKey = 0

// var requestPools = sync.Pool{
// 	New: func() interface{} {
// 		return &Request{
// 			params:  &lua.LTable{},
// 			query:   &lua.LTable{},
// 			headers: &lua.LTable{},
// 			Data:    &lua.LTable{},
// 		}
// 	},
// }

// NewRequest return lua table with http.Request representation
func NewRequest(w http.ResponseWriter, r *http.Request) *Request {
	request := &Request{
		ResponseWriter: w,
		Request:        r,
		method:         lua.LString(r.Method),
		host:           lua.LString(r.Host),
		proto:          lua.LString(r.Proto),
		path:           lua.LString(r.URL.Path),
		referer:        lua.LString(r.Referer()),
		userAgent:      lua.LString(r.UserAgent()),
		rawPath:        lua.LString(r.URL.RawPath),
		rawQuery:       lua.LString(r.URL.RawQuery),
		requestUri:     lua.LString(r.RequestURI),
		remoteAddr:     lua.LString(r.RemoteAddr),
		params:         &lua.LTable{},
		query:          &lua.LTable{},
		headers:        &lua.LTable{},
		Data:           &lua.LTable{},
	}

	if val := r.Context().Value(paramCtxKey); val != nil {
		request.params = val.(*lua.LTable)
	}

	if r.URL != nil && len(r.URL.Query()) > 0 {
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				request.query.RawSetString(k, lua.LString(v[0]))
			}
		}
	}

	if len(r.Header) > 0 {
		for k, v := range r.Header {
			if len(v) > 0 {
				request.headers.RawSetString(k, lua.LString(v[0]))
			}
		}
	}

	return request
}

func (r *Request) getMethods(L *lua.LState) *lua.LTable {
	mod := util.NewModule(L, util.Methods{
		"params":      r.params,
		"method":      r.method,
		"host":        r.host,
		"proto":       r.proto,
		"path":        r.path,
		"referer":     r.referer,
		"userAgent":   r.userAgent,
		"rawPath":     r.rawPath,
		"rawQuery":    r.rawQuery,
		"requestUri":  r.requestUri,
		"remoteAddr":  r.remoteAddr,
		"query":       r.query,
		"headers":     r.headers,
		"getQuery":    r.getQuery,
		"getHeader":   r.getHeader,
		"getCookie":   r.getCookie,
		"getBody":     r.getBody,
		"getClientIp": r.getClientIp,
		"basicAuth":   r.basicAuth,
		"postForm":    r.postForm,
		"getData":     r.getData,
		"setData":     r.setData,
		"next":        r.next,
		"error":       r.error,
	})
	return mod.Method
}

func (r *Request) next(L *lua.LState) int {
	if r.Next != nil {
		r.Next()
	}
	return 0
}

func (r *Request) getHeader(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(r.Request.Header.Get(key)))
	return 1
}

func (r *Request) getQuery(L *lua.LState) int {
	key := L.CheckString(1)
	L.Push(lua.LString(r.Request.URL.Query().Get(key)))
	return 1
}

func (r *Request) getBody(L *lua.LState) int {
	data, err := io.ReadAll(r.Request.Body)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LString(string(data)))
	return 1
}

func (r *Request) basicAuth(L *lua.LState) int {
	u, p := L.CheckString(1), L.CheckString(2)
	if user, pass, ok := r.Request.BasicAuth(); !ok || user != u || pass != p {
		L.Push(lua.LFalse)
	} else {
		L.Push(lua.LTrue)
	}
	return 1
}

func (r *Request) postForm(L *lua.LState) int {
	if err := r.Request.ParseForm(); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	lform := L.NewTable()
	for key, values := range r.Request.PostForm {
		if len(values) > 0 {
			lform.RawSetString(key, lua.LString(values[0]))
		}
	}
	L.Push(lform)
	return 1
}

func (r *Request) getClientIp(L *lua.LState) int {
	var cip string
	if ip := r.Request.Header.Get("X-Forwarded-For"); ip != "" {
		ips := strings.Split(ip, ",")
		if len(ips) > 0 {
			ip = strings.TrimSpace(ips[0])
		}
		cip = ip
	} else if ip := r.Request.Header.Get("X-Real-IP"); ip != "" {
		cip = ip
	} else {
		cip = strings.Split(r.Request.RemoteAddr, ":")[0]
	}
	L.Push(lua.LString(cip))
	return 1
}

func (r *Request) getCookie(L *lua.LState) int {
	key := L.CheckString(1)
	cookie, err := r.Request.Cookie(key)
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

func (r *Request) setData(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckAny(2)

	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
		return 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.Data.RawSetString(key, val)
	return 0
}

func (r *Request) getData(L *lua.LState) int {

	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
		return 0
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	value := r.Data.RawGetString(key)
	L.Push(value)
	return 1
}

func (r *Request) Error(err string, code int) {
	if !util.CheckStatusCode(code) {
		code = http.StatusInternalServerError
	}

	w := r.ResponseWriter
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
	}

	w.WriteHeader(code)
	if _, e := w.Write([]byte(err)); e != nil {
		http.Error(w, err, code)
	}
}

func (r *Request) error(L *lua.LState) int {
	topLen := L.GetTop()
	statusCode := http.StatusInternalServerError
	statusText := http.StatusText(statusCode)

	if topLen >= 1 {
		statusText = L.CheckString(1)
		if topLen >= 2 {
			statusCode = L.CheckInt(2)
		}
	}
	r.Error(statusText, statusCode)
	return 0
}
