package http

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type Route struct {
	host        string
	handlers    map[string]Handler
	children    map[string]*Route
	stripPrefix string
	paramName   string
	paramNode   *Route
	regex       *regexp.Regexp
	mu          sync.RWMutex
	isWild      bool
	isEnd       bool
}

var regexCache sync.Map
var notFound = "the requested path is not registered on the server"

// makeRegexp compiles and caches regular expressions to avoid redundant compilation
func makeRegexp(pattern string) *regexp.Regexp {
	if re, ok := regexCache.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}
	re := regexp.MustCompile(`^` + pattern + `$`)
	regexCache.Store(pattern, re)
	return re
}

// newNode creates and initializes a new routing node with empty collections
func newRoute() *Route {
	return &Route{
		children: make(map[string]*Route),
		handlers: make(map[string]Handler),
	}
}

// add registers a route handler for the given method and pattern
// Returns error for invalid inputs or route conflicts
func (r *Route) add(method, pattern, stripPrefix string, handler Handler) error {
	if method == "" || pattern == "" || handler == nil {
		return errors.New("http server Handle error")
	}

	pat, err := parsePattern(pattern)
	if err != nil {
		return fmt.Errorf("parsing %q: %w", pattern, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current := r
	for _, segment := range pat.segments {
		if segment.param {
			if current.paramNode == nil {
				paramNode := newRoute()
				paramNode.paramName = segment.name
				paramNode.isWild = segment.wild
				if segment.regexp != "" {
					paramNode.regex = makeRegexp(segment.regexp)
				}
				current.paramNode = paramNode
			}
			current = current.paramNode
		} else {
			child, exists := current.children[segment.name]
			if !exists {
				child = newRoute()
				current.children[segment.name] = child
			}
			current = child
		}
	}

	if _, exists := current.handlers[method]; exists {
		return fmt.Errorf("method conflict: %s %s", method, pattern)
	}

	current.isEnd = true
	current.host = pat.host
	current.handlers[method] = handler
	current.stripPrefix = stripPrefix
	return nil
}

// find traverses the routing tree to match URL segments and collect parameters
// Returns matched node or nil if no match found
func (r *Route) find(req *http.Request) (string, Handler, *lua.LTable, int, error) {

	current := r
	path, host, method := req.URL.Path, req.Host, req.Method

	params := &lua.LTable{}
	segments := strings.Split(path, `/`)

	for i, segment := range segments {
		if segment == "" && (current.paramNode == nil || !current.paramNode.isWild) {
			continue
		}

		if child, ok := current.children[segment]; ok {
			current = child
			continue
		}

		if current.paramNode != nil {
			node := current.paramNode
			name := node.paramName

			if node.regex != nil && !node.regex.MatchString(segment) {
				break
			}

			current = node

			if node.isWild {
				paramValue := strings.Join(segments[i:], "/")
				params.RawSetString(name, lua.LString(paramValue))
				break
			}

			params.RawSetString(name, lua.LString(segment))
			continue
		}

		return "", nil, nil, http.StatusNotFound, errors.New(notFound)
	}

	handler := current.handlers[method]

	switch {
	case !current.isEnd:
		return "", nil, nil, http.StatusNotFound, errors.New(notFound)

	case current.host != "" && current.host != host:
		err := fmt.Errorf("host not allowed: requested '%s', allowed '%s'", host, current.host)
		return "", nil, nil, http.StatusForbidden, err

	case handler == nil:
		if handler = current.handlers[`*`]; handler == nil {
			err := fmt.Errorf("the requested HTTP method '%s' is not supported for this path", method)
			return "", nil, nil, http.StatusInternalServerError, err
		}
	}

	return current.stripPrefix, handler, params, http.StatusOK, nil
}
