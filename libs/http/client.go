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

	"lug/pkg"
	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type client struct {
	*util.Module
	config clientConfig
}

type clientConfig struct {
	userAgent   string
	headers     http.Header
	proxy       *url.URL
	basicAuth   map[string]string
	body        []byte
	timeout     time.Duration
	keepAlive   time.Duration
	maxBodySize int64
}

type clientResponse struct {
	status   lua.LNumber
	headers  *lua.LTable
	body     lua.LString
	bodySize lua.LNumber
}

func newClient(L *lua.LState) int {

	cfg := clientConfig{
		userAgent:   pkg.Name + "/" + pkg.Version,
		headers:     make(http.Header),
		basicAuth:   make(map[string]string),
		timeout:     10 * time.Second, // 10S
		keepAlive:   60 * time.Second, // 60S
		maxBodySize: 10 * 1024 * 1024, // 10MB
	}

	if L.GetTop() >= 1 {
		opts := L.CheckTable(1)
		updateClientConfig(L, opts, &cfg)
	}

	client := &client{
		Module: util.NewModule(L),
		config: cfg,
	}

	client.SetMethods(util.Methods{
		"connect": client.handle(http.MethodConnect),
		"delete":  client.handle(http.MethodDelete),
		"get":     client.handle(http.MethodGet),
		"head":    client.handle(http.MethodHead),
		"options": client.handle(http.MethodOptions),
		"patch":   client.handle(http.MethodPatch),
		"post":    client.handle(http.MethodPost),
		"put":     client.handle(http.MethodPut),
		"trace":   client.handle(http.MethodTrace),
	})
	return client.Self()
}

func (c *client) handle(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		cfg := c.config

		url := L.CheckString(1)
		if L.GetTop() >= 2 {
			opts := L.CheckTable(2)
			updateClientConfig(L, opts, &cfg)
		}

		req, err := c.request(method, url, cfg)
		if err != nil {
			return c.NilError(err)
		}

		response, err := c.response(req, cfg)
		if err != nil {
			return c.NilError(err)
		}

		resTable := L.NewTable()
		resTable.RawSetString("status", response.status)
		resTable.RawSetString("headers", response.headers)
		resTable.RawSetString("body", response.body)
		resTable.RawSetString("body_size", response.bodySize)

		return c.Push(resTable)
	}
}

func (c *client) request(method, url string, cfg clientConfig) (*http.Request, error) {
	request, err := http.NewRequest(method, url, bytes.NewReader(cfg.body))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %v", err)
	}

	request.Header = cfg.headers.Clone()
	if cfg.userAgent != "" {
		request.Header.Set("User-Agent", cfg.userAgent)
	}

	username, password := cfg.basicAuth["username"], cfg.basicAuth["password"]
	if username != "" && password != "" {
		request.SetBasicAuth(username, password)
	}
	return request, nil
}

func (c *client) response(req *http.Request, cfg clientConfig) (*clientResponse, error) {

	transport := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		IdleConnTimeout: cfg.keepAlive,
		DialContext: (&net.Dialer{
			Timeout:   cfg.timeout,
			KeepAlive: cfg.keepAlive,
		}).DialContext,
	}

	if cfg.proxy != nil {
		transport.Proxy = http.ProxyURL(cfg.proxy)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.timeout,
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer res.Body.Close()

	response := &clientResponse{
		status:  lua.LNumber(res.StatusCode),
		headers: c.Vm.NewTable(),
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

	body, err := io.ReadAll(io.LimitReader(reader, cfg.maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	response.body = lua.LString(body)
	response.bodySize = lua.LNumber(len(body))
	return response, nil
}

func updateClientConfig(L *lua.LState, opts *lua.LTable, cfg *clientConfig) {

	opts.ForEach(func(k lua.LValue, v lua.LValue) {
		key := k.String()
		switch key {

		case `userAgent`:
			if val, ok := util.ArgLString(L, key, v); ok {
				cfg.userAgent = val
			}

		case `headers`:
			if val, ok := util.ArgLTableMap(L, key, v); ok {
				for name, value := range val {
					cfg.headers.Add(name, value)
				}
			}

		case `basicAuth`:
			if val, ok := util.ArgLTableMap(L, key, v); ok {
				for name, value := range val {
					cfg.basicAuth[name] = value
				}
			}

		case `proxy`:
			if val, ok := util.ArgLString(L, key, v); ok {
				if proxyUrl, err := url.Parse(val); err != nil {
					L.ArgError(1, "proxy must be http(s)://<username>:<password>@host:<port>")
				} else {
					cfg.proxy = proxyUrl
				}
			}

		case `timeout`:
			if val, ok := util.ArgLDuration(L, key, v); ok {
				cfg.timeout = val
			}

		case `keepAlive`:
			if val, ok := util.ArgLDuration(L, key, v); ok {
				cfg.keepAlive = val
			}

		case `body`:
			if val, ok := util.ArgLString(L, key, v); ok {
				cfg.body = []byte(val)
			}

		case `max_body_size`:
			if val, ok := util.ArgLInt64(L, key, v); ok {
				cfg.maxBodySize = val
			}
		}
	})
}
