package http

import (
	pkg "lug/package"
	"lug/util"
	"net/http"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type ResponseWriter struct {
	http.ResponseWriter
	*http.Request
	status   int
	size     int
	hijacked bool
	timedOut bool
}

func NewResponse(w http.ResponseWriter, r *http.Request) *ResponseWriter {
	response := &ResponseWriter{
		ResponseWriter: w,
		Request:        r,
		status:         http.StatusOK,
	}
	w.Header().Set(`Content-Type`, `text/html;charset=utf-8`)
	w.Header().Set(`Server`, pkg.Name+`/`+pkg.Version)
	return response
}

func (res *ResponseWriter) getMethods(L *lua.LState) *lua.LTable {
	mod := util.NewModule(L, util.Methods{
		"write":      res.write,
		"setStatus":  res.setStatus,
		"setHeader":  res.setHeader,
		"setCookie":  res.setCookie,
		"redirect":   res.redirect,
		"serveFiles": res.serveFiles,
		"serveFile":  res.serveFile,
	})
	return mod.Method
}

func (res *ResponseWriter) setStatus(L *lua.LState) int {
	code := L.CheckInt(1)
	if res.hijacked || res.timedOut {
		return 0
	}
	if util.CheckStatusCode(code) {
		res.status = code
		res.ResponseWriter.WriteHeader(code)
	}
	return 0
}

func (res *ResponseWriter) setHeader(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckString(2)
	if strings.TrimSpace(val) == "" {
		res.ResponseWriter.Header().Del(key)
	} else {
		res.ResponseWriter.Header().Set(key, val)
	}
	return 0
}

func (rw *ResponseWriter) write(L *lua.LState) int {
	body := L.CheckString(1)
	size := lua.LNumber(0)
	err := lua.LNil
	if rw.hijacked || rw.timedOut {
		err = lua.LString(http.ErrHijacked.Error())
	} else {
		if rw.status == 0 {
			rw.status = http.StatusOK
		}
		if wsize, werr := rw.ResponseWriter.Write([]byte(body)); werr != nil {
			err = lua.LString(werr.Error())
		} else {
			rw.size += wsize
			size = lua.LNumber(wsize)
		}
	}
	L.Push(size)
	L.Push(err)
	return 2
}

func (res *ResponseWriter) serveFiles(L *lua.LState) int {
	fs := http.FileServer(http.Dir(L.CheckString(1)))
	w, r := res.ResponseWriter, res.Request
	params := r.Context().Value(paramCtxKey).(*lua.LTable)
	param := params.RawGetString("*").String()
	basePath := strings.TrimSuffix(r.URL.Path, param)
	http.StripPrefix(basePath, fs).ServeHTTP(w, r)
	return 0
}

func (res *ResponseWriter) serveFile(L *lua.LState) int {
	http.ServeFile(res.ResponseWriter, res.Request, L.CheckString(1))
	return 0
}

func (res *ResponseWriter) redirect(L *lua.LState) int {
	url := L.CheckString(1)
	code := http.StatusPermanentRedirect
	if L.GetTop() >= 2 {
		if c := L.CheckInt(2); util.CheckStatusCode(c) {
			code = c
		}
	}
	http.Redirect(res.ResponseWriter, res.Request, url, code)
	return 0
}

func (res *ResponseWriter) setCookie(L *lua.LState) int {
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

	http.SetCookie(res.ResponseWriter, cookie)
	return 0
}
