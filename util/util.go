package util

import (
	lua "github.com/yuin/gopher-lua"
)

func CallLua(L *lua.LState, lfn lua.LValue, args ...interface{}) error {
	argLen := len(args)
	L.Push(lfn)
	for _, arg := range args {
		L.Push(ToLuaValue(arg))
	}
	return L.PCall(argLen, 1, nil)
}

func GetStringFromTable(L *lua.LState, t *lua.LTable, key string) string {
	v := t.RawGetString(key)
	return lua.LVAsString(v)
}

func GetIntFromTable(L *lua.LState, t *lua.LTable, key string) int {
	v := t.RawGetString(key)
	return int(lua.LVAsNumber(v))
}

func NewMod(L *lua.LState, lfs LGFunctions) *lua.LTable {
	mod := L.NewTable()
	L.SetFuncs(mod, lfs)
	return mod
}

func NewUserData(key string, L *lua.LState, lfs LGFunctions) *lua.LTable {
	ud := L.NewTypeMetatable(key)
	L.SetField(ud, "__index", L.SetFuncs(L.NewTable(), lfs))
	return ud
}

func GetUserData(L *lua.LState, key string, val interface{}) *lua.LUserData {
	ud := L.NewUserData()
	ud.Value = val
	L.SetMetatable(ud, L.GetTypeMetatable(key))

	return ud
}

func CheckStatusCode(code int) bool {
	return code >= 100 && code < 600
}
