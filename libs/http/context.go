package http

import (
	"lug/pkg"
	"net/http"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type Context struct {
	w             http.ResponseWriter
	r             *http.Request
	Data          *lua.LTable
	Params        *lua.LTable
	mu            sync.Mutex
	StartTime     time.Time
	StatusCode    int
	StatusText    string
	err           error
	Size          int
	ErrorTemplate string
	Hijacked      bool
	TimedOut      bool
	Done          chan struct{}
}

type errorData struct {
	StatusCode int
	StatusText string
	Error      string
}

var contextPool = sync.Pool{
	New: func() interface{} { return &Context{} },
}

func newContext(w http.ResponseWriter, r *http.Request) *Context {
	ctx := contextPool.Get().(*Context)

	ctx.StartTime = time.Now()
	ctx.w = w
	ctx.r = r
	ctx.Done = make(chan struct{})
	ctx.Data = &lua.LTable{}
	ctx.Params = &lua.LTable{}
	ctx.StatusCode = http.StatusOK
	ctx.StatusText = http.StatusText(http.StatusOK)
	ctx.Size = 0
	ctx.Hijacked = false
	ctx.TimedOut = false

	w.Header().Set(`Content-Type`, `text/html;charset=utf-8`)
	w.Header().Set(`Server`, pkg.Name+`/`+pkg.Version)
	return ctx
}

func (ctx *Context) Release() {
	close(ctx.Done)
	ctx.StartTime = time.Now()
	ctx.w = nil
	ctx.r = nil
	ctx.Data = nil
	ctx.Params = nil
	ctx.StatusCode = 0
	ctx.StatusText = ""
	ctx.Size = 0
	ctx.Hijacked = false
	ctx.TimedOut = false
	contextPool.Put(ctx)
}

func (ctx *Context) Since() time.Duration {
	return time.Since(ctx.StartTime)
}
