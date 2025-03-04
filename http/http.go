package http

import (
	client "github.com/geekip/lug/http/client"
	server "github.com/geekip/lug/http/server"
	lua "github.com/yuin/gopher-lua"
)

func Preload(L *lua.LState) {
	L.PreloadModule("http", Loader)
}

func Loader(L *lua.LState) int {
	mod := L.NewTable()
	mod.RawSetString("server", server.Loader(L))
	mod.RawSetString("client", client.Loader(L))
	L.Push(mod)
	return 1
}
