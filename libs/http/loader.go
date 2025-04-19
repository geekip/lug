package lHttp

import (
	"lug/libs/http/client"
	"lug/libs/http/server"
	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

func Loader(L *lua.LState) int {
	api := util.SetMethods(L, util.Methods{
		"client": client.NewClient,
		"server": server.NewServer,
	})
	return util.Push(L, api)
}
