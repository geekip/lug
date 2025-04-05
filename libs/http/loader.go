package http

import (
	lua "github.com/yuin/gopher-lua"
)

func Loader(L *lua.LState) int {
	mod := L.NewTable()
	L.SetFuncs(mod, map[string]lua.LGFunction{
		"client": newClient,
		"server": newServer,
	})
	L.Push(mod)
	return 1
}
