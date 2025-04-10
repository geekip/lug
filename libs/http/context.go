package http

import (
	"bufio"
	"lug/pkg"
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
	statusCode    int
	statusText    string
	err           error
	size          int
	errorTemplate string
	hijacked      bool
	timedOut      bool
	written       bool
	done          chan struct{}
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

	ctx.startTime = time.Now()
	ctx.response = w
	ctx.request = r
	ctx.done = make(chan struct{})
	ctx.data = &lua.LTable{}
	ctx.params = &lua.LTable{}
	ctx.statusCode = http.StatusOK
	ctx.statusText = http.StatusText(http.StatusOK)
	ctx.err = nil
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
	ctx.statusCode = 0
	ctx.statusText = ""
	ctx.err = nil
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
