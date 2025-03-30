package http

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

type pattern struct {
	path     string
	host     string
	segments []segment
}

type segment struct {
	name   string
	param  bool
	wild   bool
	regexp string
}

func parsePattern(path string) (_ *pattern, err error) {
	if len(path) == 0 {
		return nil, errors.New("empty pattern")
	}
	off := 1
	defer func() {
		if err != nil {
			err = fmt.Errorf("at offset %d: %w", off, err)
		}
	}()

	p := &pattern{path: path}

	rest := path
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return nil, errors.New("host/path missing /")
	}

	if i == 0 {
		p.host = ""
		rest = rest[0:]
	} else {
		p.host = rest[:i]
		rest = rest[i:]
		if j := strings.IndexByte(p.host, '{'); j >= 0 {
			off += j
			return nil, errors.New("host contains '{' (missing initial '/'?)")
		}
	}
	off += i
	seenNames := map[string]bool{}
	for len(rest) > 0 {
		rest = rest[1:]
		off = len(path) - len(rest)
		if len(rest) == 0 {
			// p.segments = append(p.segments, segment{name: "/",param: true, wild: true})
			break
		}
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			i = len(rest)
		}
		var seg string
		seg, rest = rest[:i], rest[i:]
		if i := strings.IndexByte(seg, '{'); i < 0 {
			seg = pathUnescape(seg)
			p.segments = append(p.segments, segment{name: seg})
		} else {
			if i != 0 {
				return nil, errors.New("bad wildcard segment (must start with '{')")
			}
			if seg[len(seg)-1] != '}' {
				return nil, errors.New("bad wildcard segment (must end with '}')")
			}
			name := seg[1 : len(seg)-1]

			name, wild := strings.CutSuffix(name, "...")
			if wild && len(rest) != 0 {
				return nil, errors.New("{...} wildcard not at end")
			}

			parts := strings.SplitN(name, `:`, 2)
			name = parts[0]
			var regex string
			if len(parts) > 1 {
				regex = parts[1]
			}

			if name == "" {
				return nil, errors.New("empty wildcard")
			}
			if !isValidWildcardName(name) {
				return nil, fmt.Errorf("bad wildcard name %q", name)
			}

			if seenNames[name] {
				return nil, fmt.Errorf("duplicate wildcard name %q", name)
			}
			seenNames[name] = true
			p.segments = append(p.segments, segment{name: name, param: true, wild: wild, regexp: regex})
		}
	}
	return p, nil
}

func isValidWildcardName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !unicode.IsLetter(c) && c != '_' && (i == 0 || !unicode.IsDigit(c)) {
			return false
		}
	}
	return true
}

func pathUnescape(path string) string {
	u, err := url.PathUnescape(path)
	if err != nil {
		return path
	}
	return u
}
