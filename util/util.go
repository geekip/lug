package util

import (
	lua "github.com/yuin/gopher-lua"
)

func CallLua(L *lua.LState, callback lua.LValue, args ...interface{}) error {
	argLen := len(args)
	L.Push(callback)
	for _, arg := range args {
		L.Push(ToLuaValue(arg))
	}
	return L.PCall(argLen, 1, nil)
}

func CheckStatusCode(code int) bool {
	return code >= 100 && code < 600
}
