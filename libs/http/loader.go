package http

import (
	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

func Loader(L *lua.LState) int {
	mod := util.NewModule(L, util.Methods{
		"server": ServerLoader(L),
		"client": ClientLoader(L),
	})
	return mod.Self()
}
