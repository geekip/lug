package http

import (
	"bufio"
	"lug/pkg"
	"lug/util"
	"net"
	"net/http"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type Context struct {
	mu            sync.Mutex
	response      http.ResponseWriter
	request       *http.Request
	data          *lua.LTable
	params        *lua.LTable
	conn          net.Conn
	bufrw         *bufio.ReadWriter
	startTime     time.Time
	stripPath     string
	statusCode    int
	statusText    string
	statusError   error
	size          int
	errorTemplate string
	hijacked      bool
	timedOut      bool
	written       bool
	done          chan struct{}
}

var contextPool = sync.Pool{
	New: func() interface{} { return &Context{} },
}

func newContext(w http.ResponseWriter, r *http.Request) *Context {
	ctx := contextPool.Get().(*Context)

	ctx.startTime = time.Now()
	ctx.response = w
	ctx.request = r
	ctx.done = make(chan struct{})
	ctx.data = &lua.LTable{}
	ctx.params = &lua.LTable{}
	ctx.stripPath = ""
	ctx.statusCode = http.StatusOK
	ctx.statusText = http.StatusText(http.StatusOK)
	ctx.statusError = nil
	ctx.size = 0
	ctx.timedOut = false
	ctx.written = false

	ctx.hijacked = false
	ctx.conn = nil
	ctx.bufrw = nil

	w.Header().Set(`Content-Type`, `text/html;charset=utf-8`)
	w.Header().Set(`Server`, pkg.Name+`/`+pkg.Version)
	return ctx
}

func (ctx *Context) Release() {
	close(ctx.done)
	ctx.startTime = time.Now()
	ctx.response = nil
	ctx.request = nil
	ctx.data = nil
	ctx.params = nil
	ctx.stripPath = ""
	ctx.statusCode = 0
	ctx.statusText = ""
	ctx.statusError = nil
	ctx.size = 0
	ctx.timedOut = false
	ctx.written = false

	ctx.hijacked = false
	ctx.conn = nil
	ctx.bufrw = nil

	contextPool.Put(ctx)
}

func (ctx *Context) Since() time.Duration {
	return time.Since(ctx.startTime)
}

func (ctx *Context) luaContext(L *lua.LState) *lua.LTable {
	r := ctx.request
	api := util.SetMethods(L, util.Methods{
		"params":     ctx.params,
		"method":     lua.LString(r.Method),
		"host":       lua.LString(r.Host),
		"proto":      lua.LString(r.Proto),
		"path":       lua.LString(r.URL.Path),
		"rawPath":    lua.LString(r.URL.RawPath),
		"rawQuery":   lua.LString(r.URL.RawQuery),
		"requestUri": lua.LString(r.RequestURI),
		"remoteAddr": lua.LString(r.RemoteAddr),
		"remoteIP":   ctx.RemoteIP,
		"referer":    ctx.Referer,
		"userAgent":  ctx.UserAgent,
		"basicAuth":  ctx.BasicAuth,
		"postForm":   ctx.PostForm,
		"get":        ctx.Get,
		"set":        ctx.Set,
		"getPath":    ctx.GetPath,
		"setPath":    ctx.SetPath,
		"getQuery":   ctx.GetQuery,
		"getHeader":  ctx.GetHeader,
		"getCookie":  ctx.GetCookie,
		"getCookies": ctx.GetCookies,
		"getBody":    ctx.GetBody,
		"getScheme":  ctx.GetScheme,
		"write":      ctx.Write,
		"setStatus":  ctx.SetStatus,
		"setHeader":  ctx.SetHeader,
		"delHeader":  ctx.DelHeader,
		"setCookie":  ctx.SetCookie,
		"delCookie":  ctx.DelCookie,
		"redirect":   ctx.Redirect,
		"serveFile":  ctx.ServeFile,
		"flush":      ctx.Flush,
		"error":      ctx.Error,
		"hijack":     ctx.Hijack,
	})
	return api
}
