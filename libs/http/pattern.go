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
	if path == "" {
		return nil, errors.New("empty pattern")
	}

	p := &pattern{path: path}
	off := 0
	defer func() {
		if err != nil {
			err = fmt.Errorf("at offset %d: %w", off, err)
		}
	}()

	// Parse host part
	rest := path
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return nil, errors.New("host/path missing /")
	}
	p.host, rest = rest[:i], rest[i:]
	off += i

	// Validate host
	if p.host != "" {
		if j := strings.IndexByte(p.host, '{'); j >= 0 {
			off = j // Host starts at offset 0 in path
			return nil, errors.New("host contains '{' (missing initial '/'?)")
		}
	}

	// Parse path segments
	seenNames := make(map[string]bool)
	for len(rest) > 0 {
		rest = rest[1:] // Skip '/'
		off = len(path) - len(rest)

		if len(rest) == 0 {
			break // Trailing slash
		}

		// Find next segment
		end := strings.IndexByte(rest, '/')
		if end < 0 {
			end = len(rest)
		}
		segStr := rest[:end]
		rest = rest[end:]

		if !strings.Contains(segStr, "{") {
			// Normal segment
			p.segments = append(p.segments, segment{
				name: pathUnescape(segStr),
			})
			continue
		}

		// Parameter segment
		if segStr[0] != '{' || segStr[len(segStr)-1] != '}' {
			return nil, errors.New("wildcard segment must be enclosed in '{...}'")
		}

		content := segStr[1 : len(segStr)-1]
		wild := false
		if strings.HasSuffix(content, "...") {
			wild = true
			content = content[:len(content)-3]
		}

		// Split name and regex
		name, regex := content, ""
		if colon := strings.IndexByte(content, ':'); colon >= 0 {
			name = content[:colon]
			regex = content[colon+1:]
		}

		switch {
		// Validate name
		case name == "":
			return nil, errors.New("empty wildcard name")

		case !isValidWildcardName(name):
			return nil, fmt.Errorf("invalid wildcard name %q", name)

		case seenNames[name]:
			return nil, fmt.Errorf("duplicate wildcard name %q", name)

		// Validate wildcard position
		case wild && len(rest) > 0:
			return nil, errors.New("{...} wildcard must be the last segment")
		}

		seenNames[name] = true

		p.segments = append(p.segments, segment{
			name:   name,
			param:  true,
			wild:   wild,
			regexp: regex,
		})
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
