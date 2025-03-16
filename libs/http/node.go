package http

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type Node struct {
	handler     Handler
	middlewares []Handler
	methods     map[string]Handler
	children    map[string]*Node
	params      *lua.LTable
	paramName   string
	paramNode   *Node
	regex       *regexp.Regexp
	mutex       sync.Mutex
	isEnd       bool
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
		methods:  make(map[string]Handler),
		params:   &lua.LTable{},
	}
}

// add registers a route handler for the given method and pattern
// Returns error for invalid inputs or route conflicts
func (n *Node) add(method, pattern string, handler Handler, middlewares []Handler) error {
	if method == "" || pattern == "" || handler == nil {
		return errors.New("http server Handle error")
	}
	n.mutex.Lock()
	defer n.mutex.Unlock()

	// if existing, ok := n.methods[method]; ok && existing != nil {
	// 	return nil, fmt.Errorf("duplicate route for method %s and pattern %s", method, pattern)
	// }

	segments := strings.Split(pattern, `/`)
	lastIndex := len(segments) - 1

	for i, segment := range segments {
		if segment == "" {
			continue
		}

		// Handle parameter segments wrapped in {}
		if strings.HasPrefix(segment, `{`) && strings.HasSuffix(segment, `}`) {
			param := segment[1 : len(segment)-1]
			parts := strings.SplitN(param, `:`, 2)
			paramName := parts[0]

			// Validate wildcard position (must be last segment)
			if strings.HasPrefix(paramName, `*`) {
				if i != lastIndex {
					return fmt.Errorf("router wildcard %s must be the last segment", segment)
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
	n.middlewares = append(n.middlewares, middlewares...)
	return nil
}

// find traverses the routing tree to match URL segments and collect parameters
// Returns matched node or nil if no match found
func (n *Node) find(method, url string) *Node {

	params := &lua.LTable{}
	segments := strings.Split(url, `/`)
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
			if strings.HasPrefix(paramName, `*`) {
				val := lua.LString(strings.Join(segments[i:], `/`))
				params.RawSetString(paramName, val)
				break
			}

			params.RawSetString(paramName, lua.LString(segment))
			continue
		}
		return nil
	}

	if n.isEnd {
		// Find method handler, fallback to wildcard if exists
		handler := n.methods[method]
		if handler == nil {
			handler = n.methods[`*`]
		}

		n.params = params
		n.handler = handler
		return n
	}
	return nil
}
