package http

import (
	"errors"
	"net/http"
	"path"
	"strings"
	"sync"

	pkg "lug/package"
)

type (
	Handler func(*Context) string
	Router  struct {
		prefix      string
		methods     []string
		node        *Node
		middlewares []Handler
	}
)

var (
	regexCache sync.Map

	defaultHeaders = map[string]string{
		"Content-Type": "text/html;charset=utf-8",
		"Server":       pkg.Name + "/" + pkg.Version,
	}
)

func newRouter() *Router {
	return &Router{node: newNode()}
}

func (r *Router) pathJoin(pattern string) string {
	if pattern == "" {
		return r.prefix
	}
	finalPath := path.Join(r.prefix, pattern)
	if strings.HasSuffix(pattern, "/") && !strings.HasSuffix(finalPath, "/") {
		return finalPath + "/"
	}
	return finalPath
}

func (r *Router) group(pattern string) *Router {
	return &Router{
		prefix:      r.pathJoin(pattern),
		node:        r.node,
		middlewares: r.middlewares,
	}
}

func (r *Router) use(middlewares ...Handler) *Router {
	if len(middlewares) == 0 {
		panic(errors.New("prue mux unkown middleware"))
	}
	r.middlewares = append(r.middlewares, middlewares...)
	return r
}

func (r *Router) method(methods ...string) *Router {
	if len(methods) == 0 {
		panic(errors.New("http server unkown http method"))
	}
	r.methods = append(r.methods, methods...)
	return r
}

func (r *Router) handle(pattern string, handler Handler) *Router {
	fullPattern := r.pathJoin(pattern)
	if len(r.methods) == 0 {
		r.methods = append(r.methods, "*")
	}
	for _, method := range r.methods {
		method = strings.ToUpper(method)
		_, err := r.node.add(method, fullPattern, handler, r.middlewares)
		if err != nil {
			panic(err)
		}
	}
	r.methods = nil
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {

	ctx := newContext(w, req)
	defer func() {
		if err := recover(); err != nil {
			ctx.Error("500 internal server error", http.StatusInternalServerError)
		}
		ctx.Release()
	}()

	for k, v := range defaultHeaders {
		w.Header().Set(k, v)
	}

	var handler Handler
	node := r.node.find(req.Method, req.URL.Path)
	if node == nil {
		ctx.Error("404 page not found", http.StatusNotFound)
		return
	}

	ctx.Params = node.params
	handler = node.handler
	if handler == nil {
		ctx.Error("405 method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if len(node.middlewares) > 0 {
		handler = r.withMiddleware(node.middlewares, handler)
	}

	body := handler(ctx)

	if !ctx.isEnd {
		ctx.Response.WriteHeader(int(ctx.Status))
		ctx.Response.Write([]byte(body))
	}

	// ctx.Release() // 确保释放
}

// Apply Middleware
func (r *Router) withMiddleware(middlewares []Handler, handler Handler) Handler {
	count := len(middlewares)
	// Insert the handler at the end of the middleware
	middlewares = append(middlewares, handler)
	for i := count; i >= 0; i-- {
		next := handler
		handler = func(c *Context) string {
			if i < count {
				// Pass next to the middleware
				c.Next = func() string { return next(c) }
			} else {
				// Remove Next from the handler
				c.Next = nil
			}
			return middlewares[i](c)
		}
	}
	return handler
}
