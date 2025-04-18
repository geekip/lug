package http

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"lug/pkg"
	"lug/util"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type Context struct {
	mu            sync.Mutex
	response      http.ResponseWriter
	request       *http.Request
	data          *lua.LTable
	route         *Route
	next          Handler
	params        *lua.LTable
	conn          net.Conn
	bufrw         *bufio.ReadWriter
	startTime     time.Time
	stripPath     string
	statusCode    int
	statusText    string
	statusError   error
	size          int
	errorTemplate string
	hijacked      bool
	timedOut      bool
	written       bool
	done          chan struct{}
}

var bufPool = sync.Pool{
	New: func() interface{} { return &bytes.Buffer{} },
}

var contextPool = sync.Pool{
	New: func() interface{} { return &Context{} },
}

func newContext(w http.ResponseWriter, r *http.Request) *Context {
	ctx := contextPool.Get().(*Context)

	ctx.startTime = time.Now()
	ctx.response = w
	ctx.request = r
	ctx.done = make(chan struct{})
	ctx.data = &lua.LTable{}
	ctx.params = &lua.LTable{}
	ctx.stripPath = ""
	ctx.statusCode = http.StatusOK
	ctx.statusText = http.StatusText(http.StatusOK)
	ctx.statusError = nil
	ctx.size = 0
	ctx.timedOut = false
	ctx.written = false

	ctx.hijacked = false
	ctx.conn = nil
	ctx.bufrw = nil

	ctx.next = nil
	ctx.route = nil

	w.Header().Set(`Content-Type`, `text/html;charset=utf-8`)
	w.Header().Set(`Server`, pkg.Name+`/`+pkg.Version)
	return ctx
}

func (ctx *Context) Release() {
	close(ctx.done)
	ctx.startTime = time.Now()
	ctx.response = nil
	ctx.request = nil
	ctx.data = nil
	ctx.params = nil
	ctx.stripPath = ""
	ctx.statusCode = 0
	ctx.statusText = ""
	ctx.statusError = nil
	ctx.size = 0
	ctx.timedOut = false
	ctx.written = false

	ctx.hijacked = false
	ctx.conn = nil
	ctx.bufrw = nil

	ctx.next = nil
	ctx.route = nil

	contextPool.Put(ctx)
}

func (ctx *Context) luaContext(L *lua.LState) *lua.LTable {
	r := ctx.request
	requestApi := util.Methods{
		"params":     ctx.params,
		"method":     lua.LString(r.Method),
		"host":       lua.LString(r.Host),
		"proto":      lua.LString(r.Proto),
		"path":       lua.LString(r.URL.Path),
		"rawPath":    lua.LString(r.URL.RawPath),
		"rawQuery":   lua.LString(r.URL.RawQuery),
		"requestUri": lua.LString(r.RequestURI),
		"remoteAddr": lua.LString(r.RemoteAddr),
		"remoteIP":   ctx.RemoteIP,
		"referer":    ctx.Referer,
		"query":      ctx.GetQuery,
		"port":       ctx.GetPort,
		"userAgent":  ctx.UserAgent,
		"basicAuth":  ctx.BasicAuth,
		"postForm":   ctx.PostForm,
		"body":       ctx.GetBody,
		"scheme":     ctx.GetScheme,
		"getData":    ctx.GetData,
		"setData":    ctx.SetData,
		"delData":    ctx.DelData,
		"getPath":    ctx.GetPath,
		"setPath":    ctx.SetPath,
		"getHeader":  ctx.GetHeader,
		"getCookie":  ctx.GetCookie,
		"getCookies": ctx.GetCookies,
		"since":      ctx.Since,
		"route":      ctx.GetRoute,
	}
	responseApi := util.Methods{
		"write":          ctx.Write,
		"setStatus":      ctx.SetStatus,
		"setHeader":      ctx.SetHeader,
		"delHeader":      ctx.DelHeader,
		"setCookie":      ctx.SetCookie,
		"delCookie":      ctx.DelCookie,
		"redirect":       ctx.Redirect,
		"serveFile":      ctx.ServeFile,
		"uploadFile":     ctx.UploadFile,
		"attachmentFile": ctx.AttachmentFile,
		"flush":          ctx.Flush,
		"error":          ctx.Error,
		"hijack":         ctx.Hijack,
	}
	return util.SetMethods(L, requestApi, responseApi)
}

func (ctx *Context) Since(L *lua.LState) int {
	elapsed := time.Since(ctx.startTime)
	microseconds := float64(elapsed.Nanoseconds()) / 1000
	milliseconds := microseconds / 1000
	return util.Push(L, lua.LNumber(milliseconds))
}

func (ctx *Context) since() time.Duration {
	return time.Since(ctx.startTime)
}

func (req *Context) Referer(L *lua.LState) int {
	return util.Push(L, lua.LString(req.request.Referer()))
}

func (req *Context) UserAgent(L *lua.LState) int {
	return util.Push(L, lua.LString(req.request.UserAgent()))
}

func (ctx *Context) SetData(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckAny(2)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	ctx.data.RawSetString(key, val)
	return 0
}

func (ctx *Context) GetData(L *lua.LState) int {
	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	return util.Push(L, ctx.data.RawGetString(key))
}

func (ctx *Context) DelData(L *lua.LState) int {
	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	ctx.data.RawSetString(key, lua.LNil)
	return util.Push(L, lua.LTrue)
}

func (req *Context) GetHeader(L *lua.LState) int {
	return util.Push(L, lua.LString(req.request.Header.Get(L.CheckString(1))))
}

func (ctx *Context) GetPath(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.request.URL.Path))
}

func (ctx *Context) SetPath(L *lua.LState) int {
	ctx.request.URL.Path = L.CheckString(1)
	return 0
}

func (ctx *Context) stripPrefix(prefix string) {
	if prefix == "" {
		return
	}
	path := ctx.request.URL.Path
	if strings.HasPrefix(path, prefix) {
		newPath := strings.TrimPrefix(path, prefix)
		if newPath == "" {
			newPath = "/"
		}
		ctx.request.URL.Path = newPath
		ctx.stripPath = newPath
	}
}

func (ctx *Context) GetRoute(L *lua.LState) int {
	route := util.SetMethods(L, util.Methods{
		"host":    lua.LString(ctx.route.host),
		"pattern": lua.LString(ctx.route.pattern),
		"methods": ctx.route.methods,
	})
	return util.Push(L, route)
}

func (ctx *Context) GetQuery(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.request.URL.Query().Get(L.CheckString(1))))
}

func (ctx *Context) GetPort(L *lua.LState) int {
	_, port, err := net.SplitHostPort(ctx.request.Host)
	if err != nil {
		if ctx.request.TLS != nil {
			port = "443"
		} else {
			port = "80"
		}
	}
	return util.Push(L, lua.LString(port))
}

func (ctx *Context) BasicAuth(L *lua.LState) int {
	u, p := L.CheckString(1), L.CheckString(2)
	user, pass, ok := ctx.request.BasicAuth()
	if !ok || user != u || pass != p {
		return util.Push(L, lua.LFalse)
	}
	return util.Push(L, lua.LTrue)
}

func (ctx *Context) GetBody(L *lua.LState) int {
	body, err := io.ReadAll(ctx.request.Body)
	if err != nil {
		return util.NilError(L, err)
	}
	defer ctx.request.Body.Close()
	return util.Push(L, lua.LString(body))
}

func (ctx *Context) PostForm(L *lua.LState) int {
	if err := ctx.request.ParseForm(); err != nil {
		return util.NilError(L, err)
	}
	lform := L.NewTable()
	for key, values := range ctx.request.PostForm {
		if len(values) > 0 {
			lform.RawSetString(key, lua.LString(values[0]))
		}
	}
	return util.Push(L, lform)
}

func (ctx *Context) RemoteIP(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.remoteIP()))
}

func (ctx *Context) remoteIP() string {
	if ip := ctx.request.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := ctx.request.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(ctx.request.RemoteAddr); err == nil {
		return host
	}
	return ctx.request.RemoteAddr
}

func (ctx *Context) transformCookie(L *lua.LState, cookie *http.Cookie) *lua.LTable {
	unparsedTable := L.NewTable()
	for _, u := range cookie.Unparsed {
		unparsedTable.Append(lua.LString(u))
	}
	lCookie := map[string]lua.LValue{
		"name":     lua.LString(cookie.Name),
		"value":    lua.LString(cookie.Value),
		"path":     lua.LString(cookie.Path),
		"domain":   lua.LString(cookie.Domain),
		"expires":  lua.LNumber(cookie.Expires.Unix()),
		"maxAge":   lua.LNumber(cookie.MaxAge),
		"secure":   lua.LBool(cookie.Secure),
		"httpOnly": lua.LBool(cookie.HttpOnly),
		"sameSite": lua.LNumber(cookie.SameSite),
		"raw":      lua.LString(cookie.Raw),
		"unparsed": unparsedTable,
	}
	lcookies := L.NewTable()
	for k, v := range lCookie {
		lcookies.RawSetString(k, v)
	}
	return lcookies
}

func (ctx *Context) GetCookie(L *lua.LState) int {
	cookie, err := ctx.request.Cookie(L.CheckString(1))
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, ctx.transformCookie(L, cookie))
}

func (ctx *Context) GetCookies(L *lua.LState) int {
	cookies := ctx.request.Cookies()
	lcookies := L.NewTable()
	for _, v := range cookies {
		lcookies.RawSetString(v.Name, ctx.transformCookie(L, v))
	}
	return util.Push(L, lcookies)
}

func (ctx *Context) GetScheme(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.getScheme()))
}

func (ctx *Context) getScheme() string {
	// Can't use `r.Request.URL.Scheme`
	// See: https://groups.google.com/forum/#!topic/golang-nuts/pMUkBlQBDF0
	header := ctx.request.Header
	if scheme := header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	if scheme := header.Get("X-Forwarded-Protocol"); scheme != "" {
		return scheme
	}
	if ssl := header.Get("X-Forwarded-Ssl"); ssl == "on" {
		return "https"
	}
	if scheme := header.Get("X-Url-Scheme"); scheme != "" {
		return scheme
	}
	return "http"
}

// sets the HTTP status code of the response.
func (ctx *Context) SetStatus(L *lua.LState) int {
	if err := ctx.setStatus(L.CheckInt(1)); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) setStatus(statusCode int) error {

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.written {
		return errors.New("superfluous response.WriteHeader")
	}
	if err := ctx.checkHijacked(); err != nil {
		return err
	}
	if !util.CheckStatusCode(statusCode) {
		return fmt.Errorf("invalid status code: %d", statusCode)
	}

	ctx.statusCode = statusCode
	ctx.statusText = http.StatusText(statusCode)

	ctx.response.WriteHeader(statusCode)
	ctx.written = true
	return nil
}

func (ctx *Context) SetHeader(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckString(2)

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.response.Header().Set(key, val)
	return 0
}

func (ctx *Context) DelHeader(L *lua.LState) int {
	key := L.CheckString(1)

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.response.Header().Del(key)
	return 0
}

func (ctx *Context) Write(L *lua.LState) int {
	if err := ctx.checkHijacked(); err != nil {
		return util.Error(L, err)
	}
	body := L.CheckString(1)

	buf := bufPool.Get().(*bytes.Buffer)
	defer bufPool.Put(buf)

	buf.Reset()
	buf.WriteString(body)

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	size, err := ctx.response.Write(buf.Bytes())
	if err != nil {
		return util.NilError(L, err)
	}

	ctx.written = true
	ctx.size += size

	return util.Push(L, lua.LNumber(ctx.size))
}

func (ctx *Context) ServeFile(L *lua.LState) int {
	if err := ctx.checkHijacked(); err != nil {
		return util.Error(L, err)
	}

	filePath := L.CheckString(1)
	lopt := L.OptTable(2, L.NewTable())

	opt := &serveFileOpts{
		ignoreBase:  false,
		autoIndex:   true,
		index:       defaultIndexes,
		prettyIndex: true,
	}

	lopt.ForEach(func(k, v lua.LValue) {
		key := k.String()
		switch key {
		case "ignoreBase":
			if val, ok := util.CheckBool(L, key, v); ok {
				opt.ignoreBase = val
			}
		case "autoIndex":
			if val, ok := util.CheckBool(L, key, v); ok {
				opt.autoIndex = val
			}
		case "index":
			if val, ok := util.CheckTable(L, key, v); ok {
				opt.index = val
			}
		case "prettyIndex":
			if val, ok := util.CheckBool(L, key, v); ok {
				opt.prettyIndex = val
			}
		}
	})

	size, statusCode, err := serveFile(ctx.response, ctx.request, filePath, ctx.stripPath, opt)
	if err != nil {
		ctx.error(statusCode, err)
		return util.Error(L, err)
	}

	ctx.size = int(size)
	return 0
}

func (ctx *Context) UploadFile(L *lua.LState) int {
	fieldName := L.CheckString(1)
	dst := L.CheckString(2)
	mode := fs.FileMode(L.OptInt(3, 0o750))
	if err := uploadFile(ctx.request, fieldName, dst, mode); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) AttachmentFile(L *lua.LState) int {
	filePath := L.CheckString(1)
	fileName := L.OptString(2, filepath.Base(filePath))
	size, statusCode, err := attachment(ctx.response, ctx.request, filePath, fileName)
	if err != nil {
		return util.Error(L, err)
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.size = int(size)
	ctx.statusCode = statusCode
	return 0
}

func (ctx *Context) Redirect(L *lua.LState) int {
	if err := ctx.checkHijacked(); err != nil {
		return util.Error(L, err)
	}

	url := L.CheckString(1)
	statusCode := L.OptInt(2, http.StatusPermanentRedirect)
	if statusCode < 300 || statusCode > 308 {
		return util.Error(L, errors.New("invalid redirect status code (300-308)"))
	}

	if ctx.written {
		return util.Error(L, errors.New("superfluous response.WriteHeader"))
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.statusCode = statusCode
	ctx.statusText = http.StatusText(statusCode)
	ctx.written = true

	http.Redirect(ctx.response, ctx.request, url, statusCode)
	return 0
}

func (ctx *Context) Flush(L *lua.LState) int {
	if err := ctx.checkHijacked(); err != nil {
		return util.Error(L, err)
	}

	flusher, ok := ctx.response.(http.Flusher)
	if ok {
		flusher.Flush()
	}
	return util.Push(L, lua.LBool(ok))
}

func (ctx *Context) Error(L *lua.LState) int {
	statusCode := L.CheckInt(1)

	var err error = nil
	if L.GetTop() > 1 {
		err = errors.New(L.CheckString(2))
	}

	if e := ctx.error(statusCode, err); e != nil {
		return util.Error(L, e)
	}
	return 0
}

func (ctx *Context) error(statusCode int, err error) error {
	if e := ctx.setStatus(statusCode); e != nil {
		return e
	}

	if statusCode < 400 || statusCode > 599 {
		return errors.New("invalid error status code (range 400–599)")
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.statusError = err

	var tpl *template.Template
	var tplErr error

	if ctx.errorTemplate == "" {
		tpl, tplErr = util.ParseTemplateString(errorTemplate, "LUG_TPL_ERRORPAGE")
	} else {
		tpl, tplErr = util.ParseTemplateFiles(ctx.errorTemplate)
	}

	if tplErr != nil {
		http.Error(ctx.response, ctx.statusText, statusCode)
		return tplErr
	}

	status := &Status{
		StatusCode:  statusCode,
		StatusText:  ctx.statusText,
		StatusError: err,
	}

	if execErr := tpl.Execute(ctx.response, status); execErr != nil {
		http.Error(ctx.response, ctx.statusText, statusCode)
		return execErr
	}

	return nil
}

func (ctx *Context) SetCookie(L *lua.LState) int {
	opts := L.CheckTable(1)
	cookie := &http.Cookie{}
	opts.ForEach(func(key, v lua.LValue) {
		k := key.String()
		switch k {
		case `name`:
			if val, ok := util.CheckString(L, k, v); ok {
				cookie.Name = val
			}
		case `value`:
			if val, ok := util.CheckString(L, k, v); ok {
				cookie.Value = val
			}
		case `path`:
			if val, ok := util.CheckString(L, k, v); ok {
				cookie.Path = val
			}
		case `domain`:
			if val, ok := util.CheckString(L, k, v); ok {
				cookie.Domain = val
			}
		case `expires`:
			if val, ok := util.CheckTime(L, k, v); ok {
				cookie.Expires = val
			}
		case `maxAge`:
			if val, ok := util.CheckInt(L, k, v); ok {
				cookie.MaxAge = val
			}
		case `secure`:
			if val, ok := util.CheckBool(L, k, v); ok {
				cookie.Secure = val
			}
		case `httpOnly`:
			if val, ok := util.CheckBool(L, k, v); ok {
				cookie.HttpOnly = val
			}
		case `sameSite`:
			sameSite, err := ctx.parseSameSite(v)
			if err != nil {
				L.ArgError(1, err.Error())
			}
			cookie.SameSite = sameSite

		default:
			L.ArgError(1, "unknown cookie field: "+k)
		}
	})

	http.SetCookie(ctx.response, cookie)
	return 0
}

func (ctx *Context) parseSameSite(v lua.LValue) (http.SameSite, error) {
	switch value := v.(type) {
	case lua.LNumber:
		code := int(value)
		if code < 0 || code > 3 {
			return 0, errors.New("invalid error sameSite (range 0-3)")
		}
		return http.SameSite(code), nil
	case lua.LString:
		switch value.String() {
		case "lax":
			return http.SameSiteLaxMode, nil
		case "strict":
			return http.SameSiteStrictMode, nil
		case "none":
			return http.SameSiteNoneMode, nil
		default:
			return http.SameSiteDefaultMode, nil
		}
	default:
		err := errors.New("sameSite must be number or string")
		return http.SameSiteDefaultMode, err
	}
}

func (ctx *Context) DelCookie(L *lua.LState) int {
	cookie, err := ctx.request.Cookie(L.CheckString(1))
	if err != nil {
		return util.Error(L, err)
	}
	cookie.MaxAge = -1
	http.SetCookie(ctx.response, cookie)
	return 0
}

func (ctx *Context) checkHijacked() error {
	switch {
	case ctx.hijacked:
		return errors.New("response already hijacked")
	case ctx.timedOut:
		return errors.New("response processing timeout")
	default:
		return nil
	}
}

func (ctx *Context) Hijack(L *lua.LState) int {

	if err := ctx.checkHijacked(); err != nil {
		return util.NilError(L, err)
	}

	if ctx.written {
		err := errors.New("superfluous response.WriteHeader")
		return util.NilError(L, err)
	}

	hiJacker, ok := ctx.response.(http.Hijacker)
	if !ok {
		err := errors.New("connection doesn't support hijacking")
		return util.NilError(L, err)
	}

	conn, bufrw, err := hiJacker.Hijack()
	if err != nil {
		return util.NilError(L, err)
	}

	ctx.mu.Lock()
	ctx.hijacked = true
	ctx.conn = conn
	ctx.bufrw = bufrw
	ctx.mu.Unlock()

	hj := util.SetMethods(L, util.Methods{
		"read":  ctx.HjRead,
		"write": ctx.HjWrite,
		"close": ctx.HjClose,
	})

	return util.Push(L, hj)
}

func (ctx *Context) HjRead(L *lua.LState) int {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if !ctx.hijacked || ctx.bufrw == nil {
		return util.NilError(L, errors.New("connection not hijacked"))
	}

	n := L.OptInt(1, 1024)
	buf := make([]byte, n)

	size, err := ctx.bufrw.Read(buf)
	if err != nil {
		return util.NilError(L, err)
	}

	return util.Push(L, lua.LString(buf[:size]))
}

func (ctx *Context) HjWrite(L *lua.LState) int {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if !ctx.hijacked || ctx.bufrw == nil {
		return util.NilError(L, errors.New("connection not hijacked"))
	}

	data := L.CheckString(1)
	size, err := ctx.bufrw.WriteString(data)
	if err != nil {
		return util.NilError(L, err)
	}
	if err := ctx.bufrw.Flush(); err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LNumber(size))
}

func (ctx *Context) HjClose(L *lua.LState) int {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.conn != nil {
		ctx.conn.Close()
		ctx.conn = nil
		ctx.bufrw = nil
		// ctx.Hijacked = false
	}
	return 0
}
