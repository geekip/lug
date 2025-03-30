package http

import (
	"errors"
	"net/http"
	"sync"
	"text/template"
	"time"

	"lug/pkg"
	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Response struct {
	ResponseWriter http.ResponseWriter
	Request        *http.Request
	StatusCode     int
	StatusText     string
	ErrorTemplate  string
	Size           int
	Hijacked       bool
	TimedOut       bool
	Params         *lua.LTable
}

type errorData struct {
	StatusCode int
	StatusText string
	Error      string
}

var responsePool = sync.Pool{
	New: func() interface{} { return &Response{} },
}

func newResponse(w http.ResponseWriter, r *http.Request) *Response {
	res := responsePool.Get().(*Response)
	res.ResponseWriter = w
	res.Request = r
	res.StatusCode = http.StatusOK
	res.StatusText = http.StatusText(http.StatusOK)
	res.Size = 0
	res.Hijacked = false
	res.TimedOut = false
	res.Params = &lua.LTable{}
	w.Header().Set(`Content-Type`, `text/html;charset=utf-8`)
	w.Header().Set(`Server`, pkg.Name+`/`+pkg.Version)
	return res
}

func (res *Response) release() {
	res.ResponseWriter = nil
	res.Request = nil
	res.StatusCode = 0
	res.StatusText = ""
	res.Size = 0
	res.Hijacked = false
	res.TimedOut = false
	res.Params = nil
	responsePool.Put(res)
}

func (res *Response) getMethods(L *lua.LState) *lua.LTable {
	methods := util.NewModule(L, util.Methods{
		"write":     res.Write,
		"setStatus": res.SetStatus,
		"setHeader": res.SetHeader,
		"delHeader": res.DelHeader,
		"setCookie": res.SetCookie,
		"redirect":  res.Redirect,
		"serveFile": res.ServeFile,
		"flush":     res.Flush,
		"error":     res.Error,
	})
	return methods.Method
}

func (res *Response) SetStatus(L *lua.LState) int {
	code := L.CheckInt(1)
	if res.Hijacked || res.TimedOut {
		return 0
	}
	if util.CheckStatusCode(code) {
		res.StatusCode = code
		res.StatusText = http.StatusText(code)
		res.ResponseWriter.WriteHeader(code)
	}
	return 0
}

func (res *Response) setStatus(code int) {
	if util.CheckStatusCode(code) {
		res.StatusCode = code
		res.StatusText = http.StatusText(code)
	}
}

func (res *Response) SetHeader(L *lua.LState) int {
	res.ResponseWriter.Header().Set(L.CheckString(1), L.CheckString(2))
	return 0
}

func (res *Response) DelHeader(L *lua.LState) int {
	res.ResponseWriter.Header().Del(L.CheckString(1))
	return 0
}

func (res *Response) Write(L *lua.LState) int {
	body := L.CheckString(1)
	var err error
	var size int
	if res.Hijacked || res.TimedOut {
		err = http.ErrHijacked
	} else {
		if res.StatusCode == 0 {
			res.setStatus(http.StatusOK)
		}
		size, err = res.ResponseWriter.Write([]byte(body))
	}
	if err != nil {
		return util.NilError(L, err)
	}
	res.Size += size
	return util.Push(L, lua.LNumber(res.Size))
}

func (res *Response) ServeFile(L *lua.LState) int {
	root := L.CheckString(1)
	opts := L.OptTable(2, L.NewTable())
	if statusCode, err := serveFile(L, root, opts, res); err != nil {
		res.error(statusCode, err)
		return util.Error(L, err)
	}
	return 0
}

func (res *Response) Redirect(L *lua.LState) int {
	url := L.CheckString(1)
	StatusCode := L.OptInt(2, http.StatusPermanentRedirect)
	if StatusCode < 300 || StatusCode > 308 {
		return util.Error(L, errors.New("invalid redirect status code"))
	}
	res.setStatus(StatusCode)
	http.Redirect(res.ResponseWriter, res.Request, url, StatusCode)
	return 0
}

func (res *Response) Flush(L *lua.LState) int {
	flusher, ok := res.ResponseWriter.(http.Flusher)
	if ok {
		flusher.Flush()
	}
	L.Push(lua.LBool(ok))
	return 1
}

func (res *Response) Error(L *lua.LState) int {
	statusCode := L.CheckInt(1)
	statusText := L.OptString(2, http.StatusText(statusCode))
	if !util.CheckStatusCode(statusCode) {
		return util.Error(L, errors.New("invalid redirect status code"))
	}
	res.error(statusCode, errors.New(statusText))
	return 0
}

func (res *Response) error(statusCode int, err error) {

	if !util.CheckStatusCode(statusCode) {
		statusCode = http.StatusInternalServerError
	}
	res.setStatus(statusCode)
	res.ResponseWriter.WriteHeader(statusCode)

	var errStr string
	if err != nil {
		errStr = err.Error()
	}

	data := &errorData{
		StatusCode: statusCode,
		StatusText: res.StatusText,
		Error:      errStr,
	}

	var tpl *template.Template
	var tplErr error

	if res.ErrorTemplate == "" {
		tpl, tplErr = util.ParseTemplateString(errorTemplate, "LUG_TPL_ERRORPAGE")
	} else {
		tpl, tplErr = util.ParseTemplateFiles(res.ErrorTemplate)
	}

	if tplErr == nil {
		tplErr = tpl.Execute(res.ResponseWriter, data)
	}

	if tplErr != nil {
		http.Error(res.ResponseWriter, res.StatusText, statusCode)
	}

}

func (res *Response) SetCookie(L *lua.LState) int {
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

		case `SameSite`:
			var SameSite http.SameSite
			switch v := val.(type) {
			case lua.LNumber:
				SameSite = http.SameSite(int(v))
			case lua.LString:
				switch v.String() {
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
			L.ArgError(1, "Unknown cookie field: "+k)

		}
	})

	http.SetCookie(res.ResponseWriter, cookie)
	return 0
}
