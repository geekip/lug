package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func httpLoader(L *lua.LState) int {
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
	L.Push(L.SetFuncs(L.NewTable(), api))
	return 1
}

// 复用HTTP客户端池
var httpClientPool = sync.Pool{
	New: func() interface{} {
		return &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   20,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
			Timeout: 30 * time.Second,
		}
	},
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
	// 解析选项参数
	params := parseOptions(L, opts)

	// 创建请求上下文（支持超时控制）
	ctx, cancel := context.WithTimeout(context.Background(), params.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(params.body))
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("create request failed: " + err.Error()))
		return 2
	}

	// 设置请求头
	for k, v := range params.headers {
		req.Header.Set(k, v)
	}

	// 添加默认UA
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", PackageName+"/"+PackageVersion)
	}

	// 从池中获取客户端
	client := httpClientPool.Get().(*http.Client)
	defer httpClientPool.Put(client)

	resp, err := client.Do(req)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("request failed: " + err.Error()))
		return 2
	}
	defer resp.Body.Close()

	// 处理响应内容
	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("gzip decode failed: " + err.Error()))
			return 2
		}
		defer reader.Close()
	default:
		reader = resp.Body
	}

	// 限制读取大小（防止内存耗尽）
	body, err := io.ReadAll(io.LimitReader(reader, params.maxBodySize))
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("read body failed: " + err.Error()))
		return 2
	}

	// 构建增强响应对象
	respTable := L.NewTable()
	headersTable := L.NewTable()
	respTable.RawSetString("body", lua.LString(body))
	respTable.RawSetString("headers", headersTable)
	respTable.RawSetString("status", lua.LNumber(resp.StatusCode))

	// 填充响应头
	for key, values := range resp.Header {
		headersTable.RawSetString(key, lua.LString(strings.Join(values, ", ")))
	}

	L.Push(respTable)
	return 1
}

// 配置参数结构
type requestParams struct {
	headers     map[string]string
	body        []byte
	timeout     time.Duration
	maxBodySize int64
}

func parseOptions(L *lua.LState, opts *lua.LTable) *requestParams {
	params := &requestParams{
		headers:     make(map[string]string),
		timeout:     30 * time.Second,
		maxBodySize: 10 * 1024 * 1024, // 10MB
	}

	// 解析headers
	if headers := L.GetField(opts, "headers"); headers != lua.LNil {
		headersTable := headers.(*lua.LTable)
		headersTable.ForEach(func(k, v lua.LValue) {
			params.headers[k.String()] = v.String()
		})
	}

	// 解析body
	if body := L.GetField(opts, "body"); body != lua.LNil {
		params.body = []byte(body.String())
	}

	// 解析timeout
	if timeout := L.GetField(opts, "timeout"); timeout != lua.LNil {
		params.timeout = time.Duration(timeout.(lua.LNumber)) * time.Second
	}

	// 解析max_body_size
	if maxBody := L.GetField(opts, "max_body_size"); maxBody != lua.LNil {
		params.maxBodySize = int64(maxBody.(lua.LNumber))
	}

	return params
}
