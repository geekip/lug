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

type Node struct {
	host      string
	handlers  map[string]Handler
	children  map[string]*Node
	paramName string
	paramNode *Node
	regex     *regexp.Regexp
	mu        sync.RWMutex
	isWild    bool
	isEnd     bool
}

var regexCache sync.Map

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
func newNode() *Node {
	return &Node{
		children: make(map[string]*Node),
		handlers: make(map[string]Handler),
	}
}

// add registers a route handler for the given method and pattern
// Returns error for invalid inputs or route conflicts
func (n *Node) add(method, pattern string, handler Handler) error {
	if method == "" || pattern == "" || handler == nil {
		return errors.New("http server Handle error")
	}

	pat, err := parsePattern(pattern)
	if err != nil {
		return fmt.Errorf("parsing %q: %w", pattern, err)
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	current := n
	for _, segment := range pat.segments {
		if segment.param {
			if current.paramNode == nil {
				paramNode := newNode()
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
				child = newNode()
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
	return nil
}

// find traverses the routing tree to match URL segments and collect parameters
// Returns matched node or nil if no match found
func (n *Node) find(r *http.Request) (Handler, *lua.LTable, int, error) {
	var notFound = "the requested path is not registered on the server"

	params := &lua.LTable{}
	segments := strings.Split(r.URL.Path, `/`)
	current := n

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
		return nil, nil, http.StatusNotFound, errors.New(notFound)
	}

	if !current.isEnd {
		return nil, nil, http.StatusNotFound, errors.New(notFound)
	}

	if current.host != "" && current.host != r.Host {
		return nil, nil, http.StatusForbidden,
			fmt.Errorf("host not allowed: requested '%s', allowed '%s'", r.Host, current.host)
	}

	handler := current.handlers[r.Method]
	if handler == nil {
		handler = current.handlers[`*`]
	}

	if handler == nil {
		return nil, nil, http.StatusInternalServerError,
			fmt.Errorf("the requested HTTP method '%s' is not supported for this path", r.Method)
	}

	return handler, params, http.StatusOK, nil
}
