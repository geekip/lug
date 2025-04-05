package libs

import (
	"lug/util"
	"net/url"

	lua "github.com/yuin/gopher-lua"
)

type Url struct{ *util.Module }

func UrlLoader(L *lua.LState) int {
	mod := &Url{
		Module: util.NewModule(L),
	}
	mod.SetMethods(util.Methods{
		"queryEscape":   mod.QueryEscape,
		"queryUnescape": mod.QueryUnescape,
		"parse":         mod.ParseURL,
		"new":           mod.BuildURL,
		"resolve":       mod.resolveURL,
	})
	return mod.Self()
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
		return u.NilError(err)
	}
	return u.Push(lua.LString(url))
}

// ParseURL lua http.parse_url(string) returns (table, err)
func (u *Url) ParseURL(L *lua.LState) int {
	U, err := url.Parse(L.CheckString(1))
	if err != nil {
		return u.NilError(err)
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

		if password, found := U.User.Password(); found {
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
		key := k.String()
		switch key {
		case `scheme`:
			if val, ok := util.CheckString(L, key, v); ok {
				U.Scheme = val
			}

		case `host`:
			if val, ok := util.CheckString(L, key, v); ok {
				U.Host = val
			}

		case `path`:
			if val, ok := util.CheckString(L, key, v); ok {
				U.Path = val
			}

		case `user`:
			username, password := "", ""
			if val, ok := util.CheckTableMap(L, key, v); ok {
				username = val[`username`]
				password = val[`password`]
			}
			U.User = url.UserPassword(username, password)

		case `query`:
			values := make(url.Values, 0)
			if value, ok := v.(*lua.LTable); ok {
				value.ForEach(func(lk lua.LValue, lv lua.LValue) {
					if value, ok := lv.(*lua.LTable); ok {
						queryValues := []string{}
						value.ForEach(func(lvk lua.LValue, lvv lua.LValue) {
							queryValues = append(queryValues, lvv.String())
						})
						values[lk.String()] = queryValues
					} else {
						L.ArgError(1, "query values must be table")
					}
				})
				U.RawQuery = values.Encode()
			} else {
				L.ArgError(1, "query must be table")
			}

		case `fragment`:
			if val, ok := util.CheckString(L, key, v); ok {
				U.Fragment = val
			}
		}

	})
	return u.Push(lua.LString(U.String()))
}

func (u *Url) resolveURL(L *lua.LState) int {
	from, to := L.CheckString(1), L.CheckString(2)

	fromUrl, err := url.Parse(from)
	if err != nil {
		return u.NilError(err)
	}

	toUrl, err := url.Parse(to)
	if err != nil {
		return u.NilError(err)
	}

	url := fromUrl.ResolveReference(toUrl).String()
	return u.Push(lua.LString(url))
}
