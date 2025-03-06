package http

import (
	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

func Loader(L *lua.LState) int {
	mod := util.GetModule(L)
	mod.Fn.RawSetString("server", ServerLoader(L))
	mod.Fn.RawSetString("client", ClientLoader(L))
	return mod.Self()
}
