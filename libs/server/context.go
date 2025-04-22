package server

import (
	"errors"
	"html/template"
	"io"
	"io/fs"
	"lug/util"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type (
	HttpStatus struct {
		Code   int
		Length int
		Text   string
		Error  error
	}
	Context struct {
		Writer        Writer
		Request       *http.Request
		Status        *HttpStatus
		data          map[string]interface{}
		Params        map[string]string
		Route         *Route
		next          Handler
		startTime     time.Time
		ErrorTemplate string
		mu            sync.RWMutex
	}
)

var contextPool = sync.Pool{
	New: func() interface{} { return &Context{} },
}

func newContext(w http.ResponseWriter, r *http.Request) *Context {
	ctx := contextPool.Get().(*Context)
	ctx.startTime = time.Now()
	ctx.Request = r
	ctx.Reset()
	ctx.Writer.Reset(w)
	return ctx
}

func (ctx *Context) Release() {
	contextPool.Put(ctx)
}

func (ctx *Context) Reset() {
	ctx.Status = &HttpStatus{
		Code: http.StatusOK,
		Text: http.StatusText(http.StatusOK),
	}
	ctx.data = make(map[string]interface{})
	ctx.Params = make(map[string]string)
	ctx.Route = nil
	ctx.next = nil
}

func (ctx *Context) luaContext(L *lua.LState) *lua.LTable {
	r := ctx.Request
	api := util.Methods{
		"params":         ctx.getParams(),
		"method":         lua.LString(r.Method),
		"host":           lua.LString(r.Host),
		"proto":          lua.LString(r.Proto),
		"path":           lua.LString(r.URL.Path),
		"rawPath":        lua.LString(r.URL.RawPath),
		"rawQuery":       lua.LString(r.URL.RawQuery),
		"requestUri":     lua.LString(r.RequestURI),
		"remoteAddr":     lua.LString(r.RemoteAddr),
		"disableCache":   ctx.disableCache,
		"remoteIP":       ctx.remoteIP,
		"referer":        ctx.referer,
		"query":          ctx.getQuery,
		"port":           ctx.getPort,
		"userAgent":      ctx.userAgent,
		"basicAuth":      ctx.basicAuth,
		"postForm":       ctx.postForm,
		"body":           ctx.getBody,
		"scheme":         ctx.getScheme,
		"getData":        ctx.getData,
		"setData":        ctx.setData,
		"delData":        ctx.delData,
		"getPath":        ctx.getPath,
		"setPath":        ctx.setPath,
		"setStatus":      ctx.setStatus,
		"getHeader":      ctx.getHeader,
		"setHeader":      ctx.setHeader,
		"delHeader":      ctx.delHeader,
		"getCookie":      ctx.getCookie,
		"getCookies":     ctx.getCookies,
		"setCookie":      ctx.setCookie,
		"delCookie":      ctx.delCookie,
		"since":          ctx.since,
		"route":          ctx.getRoute,
		"cors":           ctx.cors,
		"write":          ctx.write,
		"flush":          ctx.flush,
		"redirect":       ctx.redirect,
		"hijack":         ctx.hijack,
		"serveFile":      ctx.serveFile,
		"uploadFile":     ctx.uploadFile,
		"attachmentFile": ctx.attachmentFile,
		"error":          ctx.error,
	}
	return util.SetMethods(L, api)
}

func (ctx *Context) getParams() *lua.LTable {
	lparams := &lua.LTable{}
	for k, v := range ctx.Params {
		lparams.RawSetString(k, lua.LString(v))
	}
	return lparams
}

func (ctx *Context) since(L *lua.LState) int {
	return util.Push(L, lua.LNumber(ctx.Since()))
}

func (ctx *Context) Since() float64 {
	elapsed := time.Since(ctx.startTime)
	microseconds := float64(elapsed.Nanoseconds()) / 1000
	milliseconds := microseconds / 1000
	return milliseconds
}

func (ctx *Context) referer(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.Request.Referer()))
}

func (req *Context) userAgent(L *lua.LState) int {
	return util.Push(L, lua.LString(req.Request.UserAgent()))
}

func (ctx *Context) setData(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckAny(2)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.data[key] = val
	return 0
}

func (ctx *Context) getData(L *lua.LState) int {
	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	data, ok := ctx.data[key]
	return util.Push(L, util.ToLuaValue(data), lua.LBool(ok))
}

func (ctx *Context) delData(L *lua.LState) int {
	key := L.CheckString(1)
	if strings.TrimSpace(key) == "" {
		L.ArgError(1, "key cannot be empty")
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	delete(ctx.data, key)
	return 0
}

func (ctx *Context) getHeader(L *lua.LState) int {
	header := ctx.Request.Header.Get(L.CheckString(1))
	return util.Push(L, lua.LString(header))
}

func (ctx *Context) getPath(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.Request.URL.Path))
}

func (ctx *Context) setPath(L *lua.LState) int {
	ctx.Request.URL.Path = L.CheckString(1)
	return 0
}

func (ctx *Context) getRoute(L *lua.LState) int {
	route := util.SetMethods(L, util.Methods{
		"host":        lua.LString(ctx.Route.host),
		"pattern":     lua.LString(ctx.Route.pattern),
		"rawPath":     lua.LString(ctx.Route.rawPath),
		"stripPrefix": lua.LString(ctx.Route.stripPrefix),
		"stripPath":   lua.LString(ctx.Route.stripPath),
		"methods":     ctx.Route.methods,
	})
	return util.Push(L, route)
}

func (ctx *Context) getQuery(L *lua.LState) int {
	query := ctx.Request.URL.Query().Get(L.CheckString(1))
	return util.Push(L, lua.LString(query))
}

func (ctx *Context) getPort(L *lua.LState) int {
	_, port, err := net.SplitHostPort(ctx.Request.Host)
	if err != nil {
		if ctx.Request.TLS != nil {
			port = "443"
		} else {
			port = "80"
		}
	}
	return util.Push(L, lua.LString(port))
}

func (ctx *Context) basicAuth(L *lua.LState) int {
	user, passwd := L.CheckString(1), L.CheckString(2)
	u, p, ok := ctx.Request.BasicAuth()
	return util.Push(L, lua.LBool(!ok || user != u || passwd != p))
}

func (ctx *Context) getBody(L *lua.LState) int {
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		return util.NilError(L, err)
	}
	defer ctx.Request.Body.Close()

	return util.Push(L, lua.LString(body))
}

func (ctx *Context) postForm(L *lua.LState) int {
	if err := ctx.Request.ParseForm(); err != nil {
		return util.NilError(L, err)
	}

	form := make(map[string]string, 0)
	for key, values := range ctx.Request.PostForm {
		if len(values) > 0 {
			form[key] = values[0]
		}
	}

	lform := L.NewTable()
	for key, values := range form {
		lform.RawSetString(key, lua.LString(values))
	}
	return util.Push(L, lform)
}

func (ctx *Context) remoteIP(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.RemoteIP()))
}

func (ctx *Context) RemoteIP() string {
	if ip := ctx.Request.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := ctx.Request.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(ctx.Request.RemoteAddr); err == nil {
		return host
	}
	return ctx.Request.RemoteAddr
}

func (ctx *Context) getScheme(L *lua.LState) int {
	return util.Push(L, lua.LString(ctx.GetScheme()))
}

func (ctx *Context) GetScheme() string {
	// Can't use `r.Request.URL.Scheme`
	// See: https://groups.google.com/forum/#!topic/golang-nuts/pMUkBlQBDF0
	header := ctx.Request.Header
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

func (ctx *Context) setStatus(L *lua.LState) int {
	if err := ctx.SetStatus(L.CheckInt(1)); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) SetStatus(statusCode int) error {
	if err := ctx.Writer.WriteHeader(statusCode); err != nil {
		return err
	}
	ctx.Status.Code = statusCode
	ctx.Status.Text = http.StatusText(statusCode)
	return nil
}

func (ctx *Context) setHeader(L *lua.LState) int {
	key, val := L.CheckString(1), L.CheckString(2)
	ctx.Writer.ResponseWriter.Header().Set(key, val)
	return 0
}

func (ctx *Context) delHeader(L *lua.LState) int {
	ctx.Writer.ResponseWriter.Header().Del(L.CheckString(1))
	return 0
}

func (ctx *Context) disableCache(L *lua.LState) int {
	w := ctx.Writer.ResponseWriter
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	return 0
}

func (ctx *Context) write(L *lua.LState) int {
	body := []byte(L.CheckString(1))
	length, err := ctx.Writer.Write(body)
	if err != nil {
		return util.Error(L, err)
	}

	ctx.Status.Length += length
	return util.Push(L, lua.LNumber(length))
}

func (ctx *Context) redirect(L *lua.LState) int {
	url := L.CheckString(1)
	statusCode := L.OptInt(2, http.StatusPermanentRedirect)
	if err := ctx.Redirect(url, statusCode); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) Redirect(url string, codes ...int) error {

	if err := ctx.Writer.Written(); err != nil {
		return err
	}

	statusCode := http.StatusPermanentRedirect
	if len(codes) > 0 {
		statusCode = codes[0]
	}

	if statusCode < 300 || statusCode > 308 {
		return errors.New("invalid redirect status code (300-308)")
	}

	ctx.Status.Code = statusCode
	ctx.Status.Text = http.StatusText(statusCode)
	ctx.Writer.written = true

	http.Redirect(ctx.Writer.ResponseWriter, ctx.Request, url, statusCode)
	return nil
}

func (ctx *Context) flush(L *lua.LState) int {
	if err := ctx.Writer.Flush(); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) error(L *lua.LState) int {
	statusCode, statusError := L.CheckInt(1), L.CheckString(2)
	var err error = nil
	if L.GetTop() > 1 {
		err = errors.New(statusError)
	}
	if e := ctx.Error(statusCode, err); e != nil {
		return util.Error(L, e)
	}
	return 0
}

func (ctx *Context) Error(statusCode int, err error) error {
	if e := ctx.SetStatus(statusCode); e != nil {
		return e
	}

	if statusCode < 400 || statusCode > 599 {
		return errors.New("invalid error status code (range 400–599)")
	}

	ctx.Status.Error = err

	var tpl *template.Template
	var tplErr error

	if ctx.ErrorTemplate == "" {
		tpl, tplErr = util.ParseTemplateString(errorTemplate, "LUG_TPL_ERRORPAGE")
	} else {
		tpl, tplErr = util.ParseTemplateFiles(ctx.ErrorTemplate)
	}

	if tplErr != nil {
		http.Error(ctx.Writer.ResponseWriter, ctx.Status.Text, ctx.Status.Code)
		return tplErr
	}

	status := HttpStatus{
		Code:  statusCode,
		Text:  ctx.Status.Text,
		Error: err,
	}

	if execErr := tpl.Execute(ctx.Writer.ResponseWriter, status); execErr != nil {
		http.Error(ctx.Writer.ResponseWriter, ctx.Status.Text, statusCode)
		return execErr
	}

	return nil
}

func (ctx *Context) getCookie(L *lua.LState) int {
	cookie, err := ctx.Request.Cookie(L.CheckString(1))
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, transformCookie(L, cookie))
}

func (ctx *Context) getCookies(L *lua.LState) int {
	cookies := ctx.Request.Cookies()
	lcookies := L.NewTable()
	for _, v := range cookies {
		lcookies.RawSetString(v.Name, transformCookie(L, v))
	}
	return util.Push(L, lcookies)
}

func (ctx *Context) setCookie(L *lua.LState) int {
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
			sameSite, err := parseSameSite(v)
			if err != nil {
				L.ArgError(1, err.Error())
			}
			cookie.SameSite = sameSite

		default:
			L.ArgError(1, "unknown cookie field: "+k)
		}
	})

	http.SetCookie(ctx.Writer.ResponseWriter, cookie)
	return 0
}

func (ctx *Context) delCookie(L *lua.LState) int {
	cookie, err := ctx.Request.Cookie(L.CheckString(1))
	if err != nil {
		return util.Error(L, err)
	}
	cookie.MaxAge = -1
	http.SetCookie(ctx.Writer.ResponseWriter, cookie)
	return 0
}

func parseSameSite(v lua.LValue) (http.SameSite, error) {
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

func transformCookie(L *lua.LState, cookie *http.Cookie) *lua.LTable {
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

func (ctx *Context) hijack(L *lua.LState) int {
	if err := ctx.Writer.Hijack(); err != nil {
		return util.NilError(L, err)
	}
	hj := util.SetMethods(L, util.Methods{
		"read":  ctx.readHijack,
		"write": ctx.writeHijack,
		"close": ctx.closeHijack,
	})
	return util.Push(L, hj)
}

func (ctx *Context) readHijack(L *lua.LState) int {
	length := L.OptInt(1, 1024)
	body, err := ctx.Writer.ReadHijack(length)
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(body))
}

func (ctx *Context) writeHijack(L *lua.LState) int {
	body := L.CheckString(1)
	size, err := ctx.Writer.WriteHijack([]byte(body))
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LNumber(size))
}

func (ctx *Context) closeHijack(L *lua.LState) int {
	ctx.Writer.CloseHijack()
	return 0
}

func (ctx *Context) serveFile(L *lua.LState) int {

	filePath := L.CheckString(1)
	lopt := L.OptTable(2, L.NewTable())
	cfg := defaultFileConfig

	lopt.ForEach(func(k, v lua.LValue) {
		key := k.String()
		switch key {
		case "ignoreBase":
			if val, ok := util.CheckBool(L, key, v); ok {
				cfg.ignoreBase = val
			}
		case "autoIndex":
			if val, ok := util.CheckBool(L, key, v); ok {
				cfg.autoIndex = val
			}
		case "index":
			if val, ok := util.CheckTable(L, key, v); ok {
				cfg.index = val
			}
		case "prettyIndex":
			if val, ok := util.CheckBool(L, key, v); ok {
				cfg.prettyIndex = val
			}
		}
	})

	fileinfo, status := ctx.ServeFile(filePath, &cfg)
	if status.Error != nil {
		return util.NilError(L, status.Error)
	}
	return util.Push(L, fileInfo2Table(L, fileinfo))
}

func fileInfo2Table(L *lua.LState, fileinfo *FileInfo) *lua.LTable {
	info := util.SetMethods(L, util.Methods{
		"path":    fileinfo.Path,
		"name":    fileinfo.Name,
		"isDir":   fileinfo.IsDir,
		"modTime": fileinfo.ModTime,
		"size":    fileinfo.Size,
	})
	if list := fileinfo.List; list != nil {
		fileList := L.NewTable()
		for i := 0; i < len(list); i++ {
			fileList.Append(fileInfo2Table(L, &list[i]))
		}
		info.RawSetString("list", fileList)
	}
	return info
}

func (ctx *Context) ServeFile(filePath string, cfg *FileConfig) (*FileInfo, HttpStatus) {

	if err := ctx.Writer.Written(); err != nil {
		return nil, HttpStatus{
			Code:  http.StatusInternalServerError,
			Error: err,
		}
	}

	fs := NewFileServer(ctx.Writer.ResponseWriter, ctx.Request, cfg)
	info, status := fs.ServeFile(filePath, ctx.Route.stripPath)

	if status.Error != nil {
		ctx.Error(status.Code, status.Error)
		return nil, status
	}

	ctx.Status.Length = status.Length
	return info, status
}

func (ctx *Context) attachmentFile(L *lua.LState) int {
	filePath := L.CheckString(1)
	fileName := L.OptString(2, filepath.Base(filePath))
	fileinfo, status := ctx.AttachmentFile(filePath, fileName)
	if status.Error != nil {
		return util.Error(L, status.Error)
	}
	return util.Push(L, fileInfo2Table(L, fileinfo))
}

func (ctx *Context) cors(L *lua.LState) int {
	cfg := defaultCorsConfig
	lopt := L.OptTable(1, L.NewTable())

	lopt.ForEach(func(k lua.LValue, v lua.LValue) {
		key := k.String()
		switch key {
		case "origins":
			if val, ok := util.CheckTable(L, key, v); ok {
				if len(val) > 0 {
					cfg.origins = val
				}
			}
		case "originFunc":
			if val, ok := util.CheckFunction(L, key, v); ok {
				cfg.originFunc = invokeAllowOriginFunc(L, val)
			}
		case "methods":
			if val, ok := util.CheckTable(L, key, v); ok {
				if len(val) > 0 {
					cfg.methods = val
					cfg.hasCustomAllowMethods = true
				}
			}
		case "allowedHeaders":
			if val, ok := util.CheckTable(L, key, v); ok {
				cfg.allowedHeaders = val
			}
		case "credentials":
			if val, ok := util.CheckBool(L, key, v); ok {
				cfg.credentials = val
			}
		case "allowWildcard":
			if val, ok := util.CheckBool(L, key, v); ok {
				cfg.allowWildcard = val
			}
		case "exposeHeaders":
			if val, ok := util.CheckTable(L, key, v); ok {
				cfg.exposeHeaders = val
			}
		case "maxAge":
			if val, ok := util.CheckDuration(L, key, v); ok {
				cfg.maxAge = val
			}
		default:
			L.ArgError(1, "unknown CORS field: "+key)
		}
	})

	ctx.Cors(&cfg)
	return 0
}

func invokeAllowOriginFunc(L *lua.LState, fn *lua.LFunction) func(string) bool {
	return func(origin string) bool {
		if err := L.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, lua.LString(origin)); err != nil {
			L.RaiseError("originFunc error: %v", err)
			return false
		}

		ret := L.Get(-1)
		L.Pop(1)

		allowed, ok := ret.(lua.LBool)
		if !ok {
			L.RaiseError("originFunc must return boolean")
			return false
		}
		return allowed == lua.LTrue
	}
}

func (ctx *Context) AttachmentFile(filePath, fileName string) (*FileInfo, HttpStatus) {

	if err := ctx.Writer.Written(); err != nil {
		return nil, HttpStatus{
			Code:  http.StatusInternalServerError,
			Error: err,
		}
	}

	if fileName == "" {
		fileName = filepath.Base(filePath)
	}

	fs := NewFileServer(ctx.Writer.ResponseWriter, ctx.Request, nil)
	fileinfo, status := fs.attachment(filePath, fileName)
	if status.Error != nil {
		return nil, status
	}

	ctx.Status.Length = status.Length
	ctx.Status.Code = status.Code

	return fileinfo, status
}

func (ctx *Context) uploadFile(L *lua.LState) int {
	fieldName, dst := L.CheckString(1), L.CheckString(2)
	mode := fs.FileMode(L.OptInt(3, 0o750))
	if err := ctx.UploadFile(fieldName, dst, mode); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (ctx *Context) UploadFile(fieldName, dst string, modes ...fs.FileMode) error {
	return uploadFile(ctx.Request, fieldName, dst, modes...)
}
