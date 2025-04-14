package util

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

type Methods map[string]interface{}

func Push(L *lua.LState, args ...lua.LValue) int {
	Len := len(args)
	for i := 0; i < Len; i++ {
		L.Push(args[i])
	}
	return Len
}

func NilError(L *lua.LState, err error) int {
	L.Push(lua.LNil)
	return Error(L, err) + 1
}

func Error(L *lua.LState, err error) int {
	L.Push(lua.LString(err.Error()))
	return 1
}

func Errorf(L *lua.LState, format string, a ...any) int {
	err := fmt.Errorf(format, a...)
	return Error(L, err)
}

func RaiseError(L *lua.LState, err error) int {
	L.RaiseError(err.Error())
	return 0
}

func SetMethods(L *lua.LState, methods ...Methods) *lua.LTable {
	table := L.NewTable()
	if len(methods) == 0 {
		return table
	}
	for i := 0; i < len(methods); i++ {
		for key, val := range methods[i] {
			switch v := val.(type) {
			case lua.LGFunction:
				table.RawSetString(key, L.NewClosure(v))
			case func(*lua.LState) int:
				table.RawSetString(key, L.NewClosure(v))
			case lua.LValue:
				table.RawSetString(key, v)
			default:
				lv := ToLuaValue(v)
				table.RawSetString(key, lv)
				if lv == lua.LNil {
					err := fmt.Errorf("unsupported method type: %s", key)
					DebugPrintError(err)
				}
			}
		}
	}
	return table
}

func CallLua(L *lua.LState, callback *lua.LFunction, args ...lua.LValue) error {
	L.Push(callback)
	alen := Push(L, args...)
	return L.PCall(alen, lua.MultRet, nil)
}
