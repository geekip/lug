package http

import (
	"net/http"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type Context struct {
	w    *Response
	r    *Request
	next func()
	Done chan struct{}
}

var contextPool = sync.Pool{
	New: func() interface{} { return &Context{} },
}

func newContext(w http.ResponseWriter, r *http.Request) *Context {
	ctx := contextPool.Get().(*Context)
	ctx.w = newResponse(w, r)
	ctx.r = newRequest(r)
	ctx.next = nil
	ctx.Done = make(chan struct{})
	return ctx
}

func (ctx *Context) release() {
	close(ctx.Done)
	ctx.w.release()
	ctx.r.release()
	ctx.w = nil
	ctx.r = nil
	ctx.next = nil
	contextPool.Put(ctx)
}

func (ctx *Context) clone(w http.ResponseWriter, r *http.Request) *Context {
	ctx.w = newResponse(w, r)
	ctx.r = newRequest(r)
	return ctx
}

func (ctx *Context) Next(L *lua.LState) int {
	if ctx.next != nil {
		ctx.next()
	}
	return 0
}
