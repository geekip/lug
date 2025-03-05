package libs

import (
	"lug/util"
	"net/url"

	lua "github.com/yuin/gopher-lua"
)

type Url struct{ util.Module }

func UrlLoader(L *lua.LState) int {
	mod := &Url{
		Module: *util.GetModule(L),
	}
	api := util.LGFunctions{
		"queryEscape":   mod.QueryEscape,
		"queryUnescape": mod.QueryUnescape,
		"parse":         mod.ParseURL,
		"new":           mod.BuildURL,
		"resolve":       mod.resolveURL,
	}
	return mod.Api(api)
}

// QueryEscape lua http.query_escape(string) returns escaped string
func (u *Url) QueryEscape(L *lua.LState) int {
	query := L.CheckString(1)
	escapedUrl := url.QueryEscape(query)
	return u.Push(lua.LString(escapedUrl))
}

// QueryUnescape lua http.query_unescape(string) returns unescaped (string, error)
func (u *Url) QueryUnescape(L *lua.LState) int {
	query := L.CheckString(1)
	url, err := url.QueryUnescape(query)
	if err != nil {
		return u.Error(err)
	}
	return u.Push(lua.LString(url))
}

// ParseURL lua http.parse_url(string) returns (table, err)
func (u *Url) ParseURL(L *lua.LState) int {
	U, err := url.Parse(L.CheckString(1))
	if err != nil {
		return u.Error(err)
	}

	t := L.NewTable()
	t.RawSetString(`scheme`, lua.LString(U.Scheme))
	t.RawSetString(`host`, lua.LString(U.Host))
	t.RawSetString(`path`, lua.LString(U.Path))
	t.RawSetString(`rawQuery`, lua.LString(U.RawQuery))
	t.RawSetString(`port`, lua.LString(U.Port()))
	t.RawSetString(`fragment`, lua.LString(U.Fragment))

	// user
	if U.User != nil {
		user := L.NewTable()
		user.RawSetString(`username`, lua.LString(U.User.Username()))
		password, found := U.User.Password()
		if found {
			user.RawSetString(`password`, lua.LString(password))
		}
		t.RawSetString(`user`, user)
	}

	// query
	q := L.NewTable()
	for k, v := range U.Query() {
		values := L.NewTable()
		for _, value := range v {
			values.Append(lua.LString(value))
		}
		q.RawSetString(k, values)
	}
	t.RawSetString(`query`, q)

	return u.Push(t)
}

// BuildURL lua http.parse_url(table) returns string
func (u *Url) BuildURL(L *lua.LState) int {
	t := L.CheckTable(1)
	U := &url.URL{}
	t.ForEach(func(k lua.LValue, v lua.LValue) {
		// parse scheme
		if k.String() == `scheme` {
			if value, ok := v.(lua.LString); ok {
				U.Scheme = string(value)
			} else {
				L.ArgError(1, "scheme must be string")
			}
		}
		// parse host
		if k.String() == `host` {
			if value, ok := v.(lua.LString); ok {
				U.Host = string(value)
			} else {
				L.ArgError(1, "host must be string")
			}
		}
		// parse path
		if k.String() == `path` {
			if value, ok := v.(lua.LString); ok {
				U.Path = string(value)
			} else {
				L.ArgError(1, "path must be string")
			}
		}
		// parse user
		if k.String() == `user` {
			username, password := ``, ``
			if value, ok := v.(*lua.LTable); ok {
				username = value.RawGetString(`username`).String()
				password = value.RawGetString(`password`).String()
			} else {
				L.ArgError(1, "user must be table")
			}
			U.User = url.UserPassword(username, password)
		}
		// parse query
		if k.String() == `query` {
			values := make(url.Values, 0)
			if value, ok := v.(*lua.LTable); ok {
				value.ForEach(func(k lua.LValue, v lua.LValue) {
					if value, ok := v.(*lua.LTable); ok {
						queryValues := []string{}
						value.ForEach(func(k lua.LValue, v lua.LValue) {
							queryValues = append(queryValues, v.String())
						})
						values[k.String()] = queryValues
					} else {
						L.ArgError(1, "query values must be table")
					}
				})
				U.RawQuery = values.Encode()
			} else {
				L.ArgError(1, "query must be table")
			}
		}
		// parse fragment
		if k.String() == `fragment` {
			if value, ok := v.(lua.LString); ok {
				U.Fragment = string(value)
			} else {
				L.ArgError(1, "fragment must be string")
			}
		}

	})
	return u.Push(lua.LString(U.String()))
}

func (u *Url) resolveURL(L *lua.LState) int {
	from := L.CheckString(1)
	to := L.CheckString(2)

	fromUrl, err := url.Parse(from)
	if err != nil {
		return u.Error(err)
	}

	toUrl, err := url.Parse(to)
	if err != nil {
		return u.Error(err)
	}

	url := fromUrl.ResolveReference(toUrl).String()
	return u.Push(lua.LString(url))
}
