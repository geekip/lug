package util

import (
	"fmt"
	"math"

	lua "github.com/yuin/gopher-lua"
)

func CheckStatusCode(code int) bool {
	return code >= 100 && code < 600
}

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
	L.RaiseError(err.Error(), nil)
	return 0
}

func SetMethods(L *lua.LState, table *lua.LTable, methods ...Methods) *lua.LTable {
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

func FormatBytes(size int64) string {
	if size == 0 {
		return "0B"
	}

	sizes := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	base := 1024.0

	exp := math.Floor(math.Log(float64(size)) / math.Log(base))
	if exp > 6 {
		exp = 6
	}

	val := float64(size) / math.Pow(base, exp)
	unit := sizes[int(exp)]

	if val == math.Floor(val) {
		return fmt.Sprintf("%.0f%s", val, unit)
	} else if val < 10 {
		return fmt.Sprintf("%.2f%s", val, unit)
	} else {
		return fmt.Sprintf("%.1f%s", val, unit)
	}
}
