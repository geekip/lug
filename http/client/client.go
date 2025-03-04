package http

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	pkg "github.com/geekip/lug/package"
	lua "github.com/yuin/gopher-lua"
)

type requestParams struct {
	userAgent   string
	headers     http.Header
	proxy       *url.URL
	basicAuth   map[string]string
	body        []byte
	timeout     time.Duration
	keepAlive   time.Duration
	maxBodySize int64
}

type response struct {
	status  lua.LNumber
	headers *lua.LTable
	body    lua.LString
}

func Loader(L *lua.LState) *lua.LTable {
	mod := L.NewTable()
	api := map[string]lua.LGFunction{
		"request": apiRequest,
		"connect": apiHttp(http.MethodConnect),
		"delete":  apiHttp(http.MethodDelete),
		"get":     apiHttp(http.MethodGet),
		"head":    apiHttp(http.MethodHead),
		"options": apiHttp(http.MethodOptions),
		"patch":   apiHttp(http.MethodPatch),
		"post":    apiHttp(http.MethodPost),
		"put":     apiHttp(http.MethodPut),
		"trace":   apiHttp(http.MethodTrace),
	}
	for name, fn := range api {
		mod.RawSetString(name, L.NewFunction(fn))
	}
	return mod
}

func apiRequest(L *lua.LState) int {
	method := L.CheckString(1)
	url := L.CheckString(2)
	opts := L.OptTable(3, L.NewTable())
	return doRequest(L, method, url, opts)
}

func apiHttp(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		url := L.CheckString(1)
		opts := L.OptTable(2, L.NewTable())
		return doRequest(L, method, url, opts)
	}
}

func doRequest(L *lua.LState, method, url string, opts *lua.LTable) int {

	params := parseOptions(L, opts)
	response, err := parseResponse(L, method, url, params)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	resTable := L.NewTable()
	L.SetField(resTable, "status", response.status)
	L.SetField(resTable, "headers", response.headers)
	L.SetField(resTable, "body", response.body)

	L.Push(resTable)
	return 1
}

func parseResponse(L *lua.LState, method, URL string, params *requestParams) (*response, error) {

	req, err := createRequest(method, URL, params)
	if err != nil {
		return nil, err
	}

	client, err := createClient(params)
	if err != nil {
		return nil, err
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer res.Body.Close()

	response := &response{
		status:  lua.LNumber(res.StatusCode),
		headers: L.NewTable(),
	}

	for key, values := range res.Header {
		value := strings.Join(values, ", ")
		response.headers.RawSetString(key, lua.LString(value))
	}

	var reader io.ReadCloser
	switch res.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err := gzip.NewReader(res.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip decode failed: %v", err)
		}
		defer reader.Close()
	default:
		reader = res.Body
	}

	body, err := io.ReadAll(io.LimitReader(reader, params.maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}
	response.body = lua.LString(body)
	return response, nil
}

func createRequest(method, url string, params *requestParams) (*http.Request, error) {
	request, err := http.NewRequest(method, url, bytes.NewReader(params.body))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %v", err)
	}

	request.Header = params.headers.Clone()

	if params.userAgent != "" {
		request.Header.Set("User-Agent", params.userAgent)
	}

	username, password := params.basicAuth["username"], params.basicAuth["password"]
	if username != "" && password != "" {
		request.SetBasicAuth(username, password)
	}
	return request, nil
}

func createClient(params *requestParams) (*http.Client, error) {

	transport := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		IdleConnTimeout: params.keepAlive,
		DialContext: (&net.Dialer{
			Timeout:   params.timeout,
			KeepAlive: params.keepAlive,
		}).DialContext,
	}

	if params.proxy != nil {
		transport.Proxy = http.ProxyURL(params.proxy)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   params.timeout,
	}
	return client, nil
}

func parseOptions(L *lua.LState, opts *lua.LTable) *requestParams {

	params := &requestParams{
		userAgent:   pkg.Name + "/" + pkg.Version,
		headers:     make(http.Header),
		basicAuth:   make(map[string]string),
		timeout:     10 * time.Second, // 10S
		keepAlive:   60 * time.Second, // 60S
		maxBodySize: 10 * 1024 * 1024, // 10MB
	}

	opts.ForEach(func(k lua.LValue, v lua.LValue) {
		switch k.String() {

		case `userAgent`:
			if value, ok := v.(lua.LString); ok {
				params.userAgent = value.String()
			} else {
				L.ArgError(1, "userAgent must be string")
			}

		case `headers`:
			if value, ok := v.(*lua.LTable); ok {
				value.ForEach(func(key, val lua.LValue) {
					params.headers.Add(key.String(), val.String())
				})
			} else {
				L.ArgError(1, "headers must be table")
			}

		case `basicAuth`:
			if value, ok := v.(*lua.LTable); ok {
				value.ForEach(func(key, val lua.LValue) {
					keyStr := key.String()
					if val.Type() == lua.LTString {
						params.basicAuth[keyStr] = val.String()
					} else {
						L.ArgError(1, "basicAuth `"+keyStr+"` must be string")
					}
				})
			} else {
				L.ArgError(1, "basicAuth must be table")
			}

		case `proxy`:
			if value, ok := v.(lua.LString); ok {
				proxyUrl, err := url.Parse(value.String())
				if err == nil {
					params.proxy = proxyUrl
				} else {
					L.ArgError(1, "proxy must be http(s)://<username>:<password>@host:<port>")
				}
			} else {
				L.ArgError(1, "proxy must be string")
			}

		case `timeout`:
			if value, ok := v.(lua.LNumber); ok {
				params.timeout = time.Duration(value) * time.Millisecond
			} else {
				L.ArgError(1, "timeout must be number")
			}

		case `keepAlive`:
			if value, ok := v.(lua.LNumber); ok {
				params.keepAlive = time.Duration(value) * time.Millisecond
			} else {
				L.ArgError(1, "keepAlive must be number")
			}

		case `body`:
			if value, ok := v.(lua.LString); ok {
				params.body = []byte(string(value))
			} else {
				L.ArgError(1, "body must be string")
			}

		case `max_body_size`:
			if value, ok := v.(lua.LNumber); ok {
				params.maxBodySize = int64(value)
			} else {
				L.ArgError(1, "body must be number")
			}
		}
	})
	return params
}
