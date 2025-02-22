package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type (
	mux struct {
		methods []string
		node    *node
	}
	ctxKey int
	node   struct {
		handler   http.Handler
		methods   map[string]http.Handler
		children  map[string]*node
		params    map[string]string
		paramName string
		paramNode *node
		regex     *regexp.Regexp
		isEnd     bool
	}
	reMaps map[string]*regexp.Regexp
)

var (
	routerInstance *mux
	routerOnce     sync.Once
	keyParam       ctxKey = 0
	keyRoute       ctxKey = 1
	regexCache     reMaps = make(reMaps)
	regexCacheMu   sync.Mutex
	prefixRegexp   string = ":"
	prefixWildcard string = "*"
	prefixParam    string = "{"
	suffixParam    string = "}"
)

var rlPool = sync.Pool{
	New: func() interface{} {
		return lua.NewState()
	},
}

func routerLoader(L *lua.LState) int {
	routerOnce.Do(func() {
		routerInstance = &mux{
			node: newNode(),
		}
	})

	api := map[string]lua.LGFunction{
		"listen":  apiListen,
		"handle":  apiHandle,
		"file":    apiServeFile,
		"files":   apiServeDir,
		"all":     apiMethod("*"),
		"connect": apiMethod(http.MethodConnect),
		"delete":  apiMethod(http.MethodDelete),
		"get":     apiMethod(http.MethodGet),
		"head":    apiMethod(http.MethodHead),
		"options": apiMethod(http.MethodOptions),
		"patch":   apiMethod(http.MethodPatch),
		"post":    apiMethod(http.MethodPost),
		"put":     apiMethod(http.MethodPut),
		"trace":   apiMethod(http.MethodTrace),
	}
	L.Push(L.SetFuncs(L.NewTable(), api))
	return 1
}

func apiListen(L *lua.LState) int {
	addr := L.CheckString(1)
	err := http.ListenAndServe(addr, routerInstance)
	L.Push(lua.LString(err.Error()))
	return 1
}

func apiServeDir(L *lua.LState) int {
	pattern := L.CheckString(1)
	path := L.CheckString(2)
	routerInstance.handlerFunc(pattern, func(w http.ResponseWriter, req *http.Request) {
		params := getParams(req)
		basePath := strings.TrimSuffix(req.URL.Path, params["*"])
		http.StripPrefix(basePath, http.FileServer(http.Dir(path))).ServeHTTP(w, req)
	})
	return 0
}

func apiServeFile(L *lua.LState) int {
	pattern := L.CheckString(1)
	path := L.CheckString(2)
	routerInstance.handlerFunc(pattern, func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, path)
	})
	return 0
}

func apiHandle(L *lua.LState) int {
	return handleRoute(L, L.CheckString(1))
}

func apiMethod(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		return handleRoute(L, method)
	}
}

func handleRoute(L *lua.LState, method string) int {
	path := L.CheckString(1)
	handler := L.CheckFunction(2)

	routerInstance.method(method).handlerFunc(path, func(w http.ResponseWriter, req *http.Request) {
		RL := rlPool.Get().(*lua.LState)
		defer rlPool.Put(RL)
		RL.SetTop(0)
		// RL := lua.NewState()
		// defer RL.Close()

		RL.Push(handler)
		RL.Push(lua.LString(req.Method))
		RL.Push(lua.LString(req.URL.Path))

		params := RL.NewTable()
		for k, v := range getParams(req) {
			RL.SetField(params, k, lua.LString(v))
		}
		RL.Push(params)

		if err := RL.PCall(3, 1, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Process response
		retVal := RL.Get(-1)
		body, headers, status := parseLuaResponse(retVal)

		// Set headers
		for k, v := range headers {
			w.Header().Set(k, v)
		}

		// Send response
		w.WriteHeader(status)
		w.Write([]byte(body))
	})

	return 0
}

func parseLuaResponse(retVal lua.LValue) (string, map[string]string, int) {
	headers := map[string]string{
		"Content-Type": "text/html;charset=UTF-8",
		"Server":       PackageName + "/" + PackageVersion,
	}
	status := http.StatusOK
	var body string

	if respTable, ok := retVal.(*lua.LTable); ok {

		// Extract headers
		if headersLV := respTable.RawGetString("headers"); headersLV != lua.LNil {
			if headersTable, ok := headersLV.(*lua.LTable); ok {
				headersTable.ForEach(func(k, v lua.LValue) {
					headers[k.String()] = v.String()
				})
			}
		}
		// Extract body
		if bodyLV := respTable.RawGetString("body"); bodyLV != lua.LNil {
			body = bodyLV.String()
		}

		// Extract status code
		if statusLV := respTable.RawGetString("status"); statusLV != lua.LNil {
			if statusCode, ok := statusLV.(lua.LNumber); ok {
				status = int(statusCode)
				if status < 100 || status >= 600 {
					status = http.StatusOK
				}
			}
		}

	} else {
		body = retVal.String()
	}

	return body, headers, status
}

func (m *mux) method(methods ...string) *mux {
	if len(methods) == 0 {
		panic(errors.New("mux unkown http method"))
	}
	m.methods = append(m.methods, methods...)
	return m
}

func (m *mux) handle(pattern string, handler http.Handler) *mux {
	if len(m.methods) == 0 {
		m.methods = append(m.methods, "*")
	}
	for _, method := range m.methods {
		method = strings.ToUpper(method)
		_, err := m.node.add(method, pattern, handler)
		if err != nil {
			panic(err)
		}
	}
	m.methods = nil
	return m
}

func (m *mux) handlerFunc(pattern string, handler http.HandlerFunc) *mux {
	return m.handle(pattern, http.HandlerFunc(handler))
}

func (m *mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			http.Error(w, "500 internal server error", http.StatusInternalServerError)
		}
	}()

	node := m.node.find(r.Method, r.URL.Path)
	if node == nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	handler := node.handler
	if handler == nil {
		http.Error(w, "405 method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r = node.withContext(r)
	handler.ServeHTTP(w, r)
}

func getParams(r *http.Request) map[string]string {
	if params, ok := r.Context().Value(keyParam).(map[string]string); ok {
		return params
	}
	return nil
}

// makeRegexp compiles and caches regular expressions to avoid redundant compilation
func makeRegexp(pattern string) *regexp.Regexp {
	regexCacheMu.Lock()
	defer regexCacheMu.Unlock()

	if re, exists := regexCache[pattern]; exists {
		return re
	}
	re := regexp.MustCompile("^" + pattern + "$")
	regexCache[pattern] = re
	return re
}

// newNode creates and initializes a new routing node with empty collections
func newNode() *node {
	return &node{
		children: make(map[string]*node),
		methods:  make(map[string]http.Handler),
		params:   make(map[string]string),
	}
}

// add registers a route handler for the given method and pattern
// Returns error for invalid inputs or route conflicts
func (n *node) add(method, pattern string, handler http.Handler) (*node, error) {
	if method == "" || pattern == "" || handler == nil {
		return nil, errors.New("router handle error")
	}
	segments := strings.Split(pattern, "/")
	lastIndex := len(segments) - 1

	for i, segment := range segments {
		if segment == "" {
			continue
		}

		// Handle parameter segments wrapped in {}
		if strings.HasPrefix(segment, prefixParam) && strings.HasSuffix(segment, suffixParam) {
			param := segment[1 : len(segment)-1]
			parts := strings.SplitN(param, prefixRegexp, 2)
			paramName := parts[0]

			// Validate wildcard position (must be last segment)
			if strings.HasPrefix(paramName, prefixWildcard) {
				if i != lastIndex {
					return nil, fmt.Errorf("router wildcard %s must be the last segment", segment)
				}
			}
			// Create parameter node if not exists
			if n.paramNode == nil {
				n.paramNode = newNode()
				n.paramNode.paramName = paramName
				if len(parts) > 1 {
					n.paramNode.regex = makeRegexp(parts[1])
				}
			}
			n = n.paramNode
		} else {
			// Add static path segment to routing tree
			child, exists := n.children[segment]
			if !exists {
				child = newNode()
				n.children[segment] = child
			}
			n = child
		}
	}

	n.isEnd = true
	n.methods[method] = handler
	return n, nil
}

// find traverses the routing tree to match URL segments and collect parameters
// Returns matched node or nil if no match found
func (n *node) find(method, url string) *node {
	params := make(map[string]string)
	segments := strings.Split(url, "/")
	for i, segment := range segments {
		if segment == "" {
			continue
		}

		// Try static path match first
		if child := n.children[segment]; child != nil {
			n = child
			continue
		}

		// Fallback to parameter matching
		if n.paramNode != nil {
			paramNode := n.paramNode
			paramName := paramNode.paramName

			// Validate against regex constraint if present
			if paramNode.regex != nil && !paramNode.regex.MatchString(segment) {
				return nil
			}

			n = paramNode

			// Handle wildcard parameter (capture remaining path segments)
			if strings.HasPrefix(paramName, prefixWildcard) {
				params[paramName] = strings.Join(segments[i:], "/")
				break
			}

			params[paramName] = segment
			continue
		}
		return nil
	}

	if n.isEnd {
		// Find method handler, fallback to wildcard if exists
		handler := n.methods[method]
		if handler == nil {
			handler = n.methods[prefixWildcard]
		}

		n.params = params
		n.handler = handler
		return n
	}
	return nil
}

// withContext injects route parameters and current node into request context
func (n *node) withContext(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), keyRoute, n)
	if len(n.params) > 0 {
		ctx = context.WithValue(ctx, keyParam, n.params)
	}
	return r.WithContext(ctx)
}
