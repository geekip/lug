package server

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
)

type Route struct {
	host        string
	pattern     string
	params      MapString
	methods     []string
	handler     Handler
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

var (
	regexCache sync.Map
	notFound   = "the requested path is not registered on the server"
)

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
		return fmt.Errorf("parsing pattern %q: %w", pattern, err)
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
	current.pattern = pattern
	current.handlers[method] = handler
	current.stripPrefix = stripPrefix

	return nil
}

// find traverses the routing tree to match URL segments and collect parameters
// Returns matched node or nil if no match found
func (r *Route) find(req *http.Request) (*Route, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	urlPath, host, method := req.URL.Path, req.Host, req.Method
	segments := strings.Split(urlPath, `/`)
	params := make(map[string]string)
	current := r

	for i := 0; i < len(segments); i++ {
		segment := segments[i]

		if segment == "" {
			paramNode := current.paramNode
			if paramNode == nil || !paramNode.isWild {
				continue
			}
		}

		if child, exists := current.children[segment]; exists {
			current = child
			continue
		}

		if paramNode := current.paramNode; paramNode != nil {
			regex := paramNode.regex
			if regex != nil && !regex.MatchString(segment) {
				break
			}

			current = paramNode
			name := current.paramName

			if paramNode.isWild {
				params[name] = path.Join(segments[i:]...)
				break
			}
			params[name] = segment
			continue
		}

		return nil, http.StatusNotFound, errors.New(notFound)
	}

	if !current.isEnd {
		return nil, http.StatusNotFound, errors.New(notFound)
	}

	if current.host != "" && current.host != host {
		err := fmt.Errorf("host not allowed: requested '%s', allowed '%s'", host, current.host)
		return nil, http.StatusNotFound, err
	}

	handler := current.handlers[method]
	if handler == nil {
		if handler = current.handlers["*"]; handler == nil {
			err := fmt.Errorf("the requested HTTP method '%s' is not supported for this path", method)
			return nil, http.StatusMethodNotAllowed, err
		}
	}
	current.handler = handler

	methods := make([]string, 0)
	for method := range current.handlers {
		if method == "*" {
			methods = allowMethods
			break
		}
		methods = append(methods, method)
	}
	current.methods = methods
	current.params = params

	return current, http.StatusOK, nil
}
