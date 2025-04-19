package server

import (
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type corsConfig struct {
	origins                []string
	originFunc             func(string) bool
	methods                []string
	credentials            bool
	allowWildcard          bool
	allowedHeaders         []string
	exposeHeaders          []string
	maxAge                 time.Duration
	compiledOriginPatterns []*regexp.Regexp
	hasCustomAllowMethods  bool
}

const (
	hdrVary       = "Vary"
	hdrACRMethod  = "Access-Control-Request-Method"
	hdrACRHeaders = "Access-Control-Request-Headers"
	hdrACAO       = "Access-Control-Allow-Origin"
	hdrACAMethods = "Access-Control-Allow-Methods"
	hdrACAHeaders = "Access-Control-Allow-Headers"
	hdrACACred    = "Access-Control-Allow-Credentials"
	hdrACExpose   = "Access-Control-Expose-Headers"
	hdrACMaxAge   = "Access-Control-Max-Age"
)

var defaultCorsConfig = corsConfig{
	origins:        []string{"*"},
	methods:        allowMethods,
	allowedHeaders: []string{"Origin", "Content-Length", "Content-Type"},
	maxAge:         86400 * time.Second,
}

func (ctx *Context) Cors(opt ...corsConfig) {
	cfg := &defaultCorsConfig
	if len(opt) > 0 {
		cfg = &opt[0]
	}
	cfg.compiledOriginPatterns = compileOriginPatterns(cfg.origins)

	w, r := ctx.response, ctx.request
	origin := r.Header.Get("Origin")

	w.Header().Add(hdrVary, "Origin")
	preflight := r.Method == http.MethodOptions

	if origin == "" {
		if preflight {
			ctx.statusCode = http.StatusOK
		}
		return
	}

	allowOrigin := ""
	if cfg.originFunc != nil {
		if cfg.originFunc(origin) {
			allowOrigin = origin
		}
	} else {
		allowOrigin = checkAllowedOrigins(cfg, origin)
	}

	if allowOrigin == "" {
		if preflight {
			ctx.statusCode = http.StatusOK
		}
		return
	}

	setCorsHeaders(w, cfg, allowOrigin, preflight, ctx)
}

func checkAllowedOrigins(cfg *corsConfig, origin string) string {
	for _, o := range cfg.origins {
		if o == "*" {
			if cfg.credentials && cfg.allowWildcard {
				return origin
			}
			return o
		}
		if o == origin || matchSubdomain(origin, o) {
			return origin
		}
	}

	if strings.Contains(origin, "://") {
		for _, re := range cfg.compiledOriginPatterns {
			if re.MatchString(origin) {
				return origin
			}
		}
	}
	return ""
}

func setCorsHeaders(w http.ResponseWriter, cfg *corsConfig, allowOrigin string, preflight bool, ctx *Context) {
	w.Header().Set(hdrACAO, allowOrigin)
	if cfg.credentials {
		w.Header().Set(hdrACACred, "true")
	}

	if !preflight {
		if len(cfg.exposeHeaders) > 0 {
			w.Header().Set(hdrACExpose, strings.Join(cfg.exposeHeaders, ","))
		}
		return
	}

	w.Header().Add(hdrVary, hdrACRMethod)
	w.Header().Add(hdrVary, hdrACRHeaders)

	methods := strings.Join(cfg.methods, ",")
	if !cfg.hasCustomAllowMethods {
		if routerMethods, ok := ctx.getStore("LUG_ALLOW_METHODS").(string); ok {
			methods = routerMethods
		}
	}
	w.Header().Set(hdrACAMethods, methods)

	if len(cfg.allowedHeaders) > 0 {
		w.Header().Set(hdrACAHeaders, strings.Join(cfg.allowedHeaders, ","))
	} else if h := ctx.request.Header.Get(hdrACRHeaders); h != "" {
		w.Header().Set(hdrACAHeaders, h)
	}

	if cfg.maxAge > time.Duration(0) {
		maxAge := strconv.FormatInt(int64(cfg.maxAge/time.Second), 10)
		w.Header().Set(hdrACMaxAge, maxAge)
	}
}

func compileOriginPatterns(origins []string) []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, 0, len(origins))
	for _, origin := range origins {
		if origin == "*" {
			continue
		}
		pattern := regexp.QuoteMeta(origin)
		pattern = strings.ReplaceAll(pattern, "\\*", ".*")
		pattern = strings.ReplaceAll(pattern, "\\?", ".")
		pattern = "^" + pattern + "$"

		if re, err := regexp.Compile(pattern); err == nil {
			patterns = append(patterns, re)
		}
	}
	return patterns
}

func matchSubdomain(domain, pattern string) bool {

	// matchScheme
	if !strings.HasPrefix(domain, strings.SplitN(pattern, ":", 2)[0]) {
		return false
	}

	domParts := strings.SplitN(domain, "://", 2)
	patParts := strings.SplitN(pattern, "://", 2)
	if len(domParts) != 2 || len(patParts) != 2 {
		return false
	}

	domHost, _, err := net.SplitHostPort(domParts[1])
	if err != nil {
		domHost = domParts[1]
	}

	patHost, _, err := net.SplitHostPort(patParts[1])
	if err != nil {
		patHost = patParts[1]
	}

	domComps := strings.Split(domHost, ".")
	patComps := strings.Split(patHost, ".")

	if len(patComps) > len(domComps) {
		return false
	}

	for i := 1; i <= len(patComps); i++ {
		domIdx := len(domComps) - i
		patIdx := len(patComps) - i
		if patComps[patIdx] != "*" && patComps[patIdx] != domComps[domIdx] {
			return false
		}
	}
	return true
}
