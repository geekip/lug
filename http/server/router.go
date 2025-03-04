package http

import (
	"errors"
	"net/http"
	"path"
	"strings"
	"sync"

	pkg "github.com/geekip/lug/package"
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

func (m *Router) pathJoin(pattern string) string {
	if pattern == "" {
		return m.prefix
	}
	finalPath := path.Join(m.prefix, pattern)
	if strings.HasSuffix(pattern, "/") && !strings.HasSuffix(finalPath, "/") {
		return finalPath + "/"
	}
	return finalPath
}

func (m *Router) group(pattern string) *Router {
	return &Router{
		prefix:      m.pathJoin(pattern),
		node:        m.node,
		middlewares: m.middlewares,
	}
}

func (m *Router) use(middlewares ...Handler) *Router {
	if len(middlewares) == 0 {
		panic(errors.New("prue mux unkown middleware"))
	}
	m.middlewares = append(m.middlewares, middlewares...)
	return m
}

func (m *Router) method(methods ...string) *Router {
	if len(methods) == 0 {
		panic(errors.New("http server unkown http method"))
	}
	m.methods = append(m.methods, methods...)
	return m
}

func (m *Router) handle(pattern string, handler Handler) *Router {
	fullPattern := m.pathJoin(pattern)
	if len(m.methods) == 0 {
		m.methods = append(m.methods, "*")
	}
	for _, method := range m.methods {
		method = strings.ToUpper(method)
		_, err := m.node.add(method, fullPattern, handler, m.middlewares)
		if err != nil {
			panic(err)
		}
	}
	m.methods = nil
	return m
}

func (m *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	ctx := newContext(w, r)
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
	node := m.node.find(r.Method, r.URL.Path)
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
		handler = m.withMiddleware(node.middlewares, handler)
	}

	body := handler(ctx)

	if !ctx.isEnd {
		ctx.Response.WriteHeader(int(ctx.Status))
		ctx.Response.Write([]byte(body))
	}

	// ctx.Release() // 确保释放
}

// Apply Middleware
func (m *Router) withMiddleware(middlewares []Handler, handler Handler) Handler {
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
