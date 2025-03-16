package http

import (
	lua "github.com/yuin/gopher-lua"
)

func Loader(L *lua.LState) int {
	mod := L.NewTable()
	mod.RawSetString("client", newClient(L))
	mod.RawSetString("server", newServer(L))
	L.Push(mod)
	return 1
}
