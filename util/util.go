package util

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

type LMethods map[string]interface{}

func SetMethods(L *lua.LState, methods ...LMethods) *lua.LTable {
	tb := L.NewTable()
	if len(methods) == 0 {
		return tb
	}
	for i := 0; i < len(methods); i++ {
		for key, val := range methods[i] {
			switch v := val.(type) {
			case lua.LGFunction:
				tb.RawSetString(key, L.NewClosure(v))
			case func(*lua.LState) int:
				tb.RawSetString(key, L.NewClosure(v))
			case lua.LValue:
				tb.RawSetString(key, v)
			default:
				lv := ToLuaValue(v)
				tb.RawSetString(key, lv)
				if lv == lua.LNil {
					err := fmt.Errorf("unsupported method type: %s", key)
					fmt.Println(err)
					// DebugPrintError(err)
				}
			}
		}
	}
	return tb
}

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
