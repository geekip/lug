package server

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
)

type Map map[string]interface{}
type MapString map[string]string

type Context struct {
	mu            sync.RWMutex
	response      http.ResponseWriter
	request       *http.Request
	data          Map
	route         *Route
	Next          Handler
	params        MapString
	conn          net.Conn
	bufrw         *bufio.ReadWriter
	startTime     time.Time
	store         Map
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

func (ctx *Context) getStore(key string) interface{} {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.store[key]
}

func (ctx *Context) setStore(key string, val interface{}) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.store == nil {
		ctx.store = make(Map)
	}
	ctx.store[key] = val
}

func newContext(w http.ResponseWriter, r *http.Request) *Context {
	ctx := contextPool.Get().(*Context)

	ctx.startTime = time.Now()
	ctx.response = w
	ctx.request = r
	ctx.done = make(chan struct{})
	ctx.data = make(Map)
	ctx.params = make(MapString)
	ctx.stripPath = ""
	ctx.store = make(Map)
	ctx.statusCode = http.StatusOK
	ctx.statusText = http.StatusText(http.StatusOK)
	ctx.statusError = nil
	ctx.size = 0
	ctx.timedOut = false
	ctx.written = false

	ctx.hijacked = false
	ctx.conn = nil
	ctx.bufrw = nil

	ctx.Next = nil
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
	ctx.store = nil
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

	ctx.Next = nil
	ctx.route = nil

	contextPool.Put(ctx)
}

func (ctx *Context) Since() float64 {
	elapsed := time.Since(ctx.startTime)
	microseconds := float64(elapsed.Nanoseconds()) / 1000
	milliseconds := microseconds / 1000
	return milliseconds
}

func (ctx *Context) SetData(key string, val interface{}) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.data[key] = val
}

func (ctx *Context) GetData(key string) (v interface{}, ok bool) {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	v, ok = ctx.data[key]
	return
}

func (ctx *Context) DelData(key string) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	delete(ctx.data, key)
}

func (req *Context) GetHeader(key string) string {
	return req.request.Header.Get(key)
}

func (ctx *Context) GetPath() string {
	return ctx.request.URL.Path
}

func (ctx *Context) SetPath(path string) {
	ctx.request.URL.Path = path
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

func (ctx *Context) GetQuery(key string) string {
	return ctx.request.URL.Query().Get(key)
}

func (ctx *Context) GetPort() string {
	_, port, err := net.SplitHostPort(ctx.request.Host)
	if err != nil {
		if ctx.request.TLS != nil {
			port = "443"
		} else {
			port = "80"
		}
	}
	return port
}

func (ctx *Context) BasicAuth(user, pass string) bool {
	u, p, ok := ctx.request.BasicAuth()
	return !ok || user != u || pass != p
}

func (ctx *Context) GetBody() ([]byte, error) {
	body, err := io.ReadAll(ctx.request.Body)
	if err != nil {
		return nil, err
	}
	defer ctx.request.Body.Close()
	return body, nil
}

func (ctx *Context) PostForm() (MapString, error) {
	if err := ctx.request.ParseForm(); err != nil {
		return nil, err
	}
	form := make(MapString, 0)
	for key, values := range ctx.request.PostForm {
		if len(values) > 0 {
			form[key] = values[0]
		}
	}
	return form, nil
}

func (ctx *Context) RemoteIP() string {
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

func (ctx *Context) GetScheme() string {
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

func (ctx *Context) SetStatus(statusCode int) error {

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.written {
		return errors.New("superfluous response.WriteHeader")
	}
	if err := ctx.checkHijacked(); err != nil {
		return err
	}
	if statusCode == 0 {
		statusCode = http.StatusNoContent
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

func (ctx *Context) SetHeader(key, val string) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.response.Header().Set(key, val)
}

func (ctx *Context) DelHeader(key string) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.response.Header().Del(key)
}

func (ctx *Context) DisableCache() {
	ctx.response.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	ctx.response.Header().Set("Pragma", "no-cache")
	ctx.response.Header().Set("Expires", "0")
}

func (ctx *Context) Write(body []byte) (int, error) {
	if err := ctx.checkHijacked(); err != nil {
		return 0, err
	}

	buf := bufPool.Get().(*bytes.Buffer)
	defer bufPool.Put(buf)

	buf.Reset()
	buf.Write(body)

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	size, err := ctx.response.Write(buf.Bytes())
	if err != nil {
		return 0, err
	}

	ctx.written = true
	ctx.size += size

	return ctx.size, nil
}

func (ctx *Context) Redirect(url string, codes ...int) error {
	if err := ctx.checkHijacked(); err != nil {
		return err
	}
	statusCode := http.StatusPermanentRedirect
	if len(codes) > 0 {
		statusCode = codes[0]
	}

	if statusCode < 300 || statusCode > 308 {
		return errors.New("invalid redirect status code (300-308)")
	}

	if ctx.written {
		return errors.New("superfluous response.WriteHeader")
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.statusCode = statusCode
	ctx.statusText = http.StatusText(statusCode)
	ctx.written = true

	http.Redirect(ctx.response, ctx.request, url, statusCode)
	return nil
}

func (ctx *Context) Flush() error {
	if err := ctx.checkHijacked(); err != nil {
		return err
	}
	if flusher, ok := ctx.response.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func (ctx *Context) Error(statusCode int, err error) error {
	if e := ctx.SetStatus(statusCode); e != nil {
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

	status := &httpStatus{
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

func (ctx *Context) GetCookie(key string) (*http.Cookie, error) {
	return ctx.request.Cookie(key)
}

func (ctx *Context) GetCookies() []*http.Cookie {
	return ctx.request.Cookies()
}

func (ctx *Context) SetCookie(cookie *http.Cookie) {
	http.SetCookie(ctx.response, cookie)
}

func (ctx *Context) DelCookie(key string) error {
	cookie, err := ctx.request.Cookie(key)
	if err != nil {
		return err
	}
	cookie.MaxAge = -1
	http.SetCookie(ctx.response, cookie)
	return nil
}

func (ctx *Context) Hijack() error {

	if err := ctx.checkHijacked(); err != nil {
		return err
	}

	if ctx.written {
		return errors.New("superfluous response.WriteHeader")
	}

	hiJacker, ok := ctx.response.(http.Hijacker)
	if !ok {
		return errors.New("connection doesn't support hijacking")
	}

	conn, bufrw, err := hiJacker.Hijack()
	if err != nil {
		return err
	}

	ctx.mu.Lock()
	ctx.hijacked = true
	ctx.conn = conn
	ctx.bufrw = bufrw
	ctx.mu.Unlock()

	return nil
}

func (ctx *Context) ReadHijack(size ...int) ([]byte, error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if !ctx.hijacked || ctx.bufrw == nil {
		return nil, errors.New("connection not hijacked")
	}
	s := 1024
	if len(size) > 0 {
		s = size[0]
	}
	buf := make([]byte, s)

	n, err := ctx.bufrw.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (ctx *Context) WriteHijack(body []byte) (int, error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if !ctx.hijacked || ctx.bufrw == nil {
		return 0, errors.New("connection not hijacked")
	}

	size, err := ctx.bufrw.Write(body)
	if err != nil {
		return 0, err
	}
	if err := ctx.bufrw.Flush(); err != nil {
		return 0, err
	}
	return size, nil
}

func (ctx *Context) CloseHijack() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.conn != nil {
		ctx.conn.Close()
		ctx.conn = nil
		ctx.bufrw = nil
		// ctx.Hijacked = false
	}
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

func (ctx *Context) ServeFile(filePath string, opt ...*serveFileOpts) httpStatus {
	if err := ctx.checkHijacked(); err != nil {
		return httpStatus{
			StatusCode:  http.StatusInternalServerError,
			StatusError: err,
		}
	}

	o := &defaultServeFileOpts
	if len(opt) > 0 {
		o = opt[0]
	}

	status := serveFile(ctx.response, ctx.request, filePath, ctx.stripPath, o)
	if status.StatusError != nil {
		ctx.Error(status.StatusCode, status.StatusError)
		return status
	}

	ctx.size = int(status.StatusSize)
	return status
}

func (ctx *Context) UploadFile(fieldName, dst string, modes ...fs.FileMode) error {
	return uploadFile(ctx.request, fieldName, dst, modes...)
}

func (ctx *Context) AttachmentFile(filePath string, fileNames ...string) httpStatus {
	fileName := filepath.Base(filePath)
	if len(fileNames) > 0 {
		fileName = fileNames[0]
	}
	status := attachment(ctx.response, ctx.request, filePath, fileName)
	if status.StatusError != nil {
		return status
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.size = int(status.StatusSize)
	ctx.statusCode = status.StatusCode
	return status
}
