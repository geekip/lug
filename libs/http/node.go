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
	host        string
	regex       *regexp.Regexp
	mu          sync.Mutex
	isEnd       bool
	isWild      bool
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
	n.mu.Lock()
	defer n.mu.Unlock()

	pat, err := parsePattern(pattern)
	if err != nil {
		return fmt.Errorf("parsing %q: %w", pattern, err)
	}

	for _, segment := range pat.segments {
		if segment.param {
			if n.paramNode == nil {
				pn := newNode()
				pn.paramName = segment.name
				pn.isWild = segment.wild
				if segment.regexp != "" {
					pn.regex = makeRegexp(segment.regexp)
				}
				n.paramNode = pn
			}
			n = n.paramNode
		} else {
			child, exists := n.children[segment.name]
			if !exists {
				child = newNode()
				n.children[segment.name] = child
			}
			n = child
		}
	}

	n.host = pat.host
	n.isEnd = true
	n.methods[method] = handler
	n.middlewares = append(n.middlewares, middlewares...)
	return nil
}

// find traverses the routing tree to match URL segments and collect parameters
// Returns matched node or nil if no match found
func (n *Node) find(method, url string) *Node {
	params := n.params
	segments := strings.Split(url, `/`)

	for i, segment := range segments {

		if segment == "" && (n.paramNode == nil || !n.paramNode.isWild) {
			continue
		}

		// static path
		if child := n.children[segment]; child != nil {
			n = child
			if wild := n.findWild(params, segments, i); wild != nil {
				n = wild
				break
			}
			continue
		}

		// param path
		if pn := n.paramNode; pn != nil {
			n = pn
			if n.regex != nil && !n.regex.MatchString(segment) {
				return nil
			}

			params.RawSetString(n.paramName, lua.LString(segment))

			if wild := n.findWild(params, segments, i); wild != nil {
				n = wild
				break
			}
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

func (n *Node) findWild(params *lua.LTable, segments []string, index int) *Node {
	nextIndex := index + 1

	if !n.isWild {
		n = n.paramNode
	}

	if n != nil && n.isWild {
		if nextIndex == len(segments) {
			return nil
		}
		n.isEnd = true
		param := strings.Join(segments[nextIndex:], `/`)
		params.RawSetString(n.paramName, lua.LString(param))
		return n
	}

	return nil
}
