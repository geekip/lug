package json

import (
	"encoding/json"

	"github.com/geekip/lug/util"
	lua "github.com/yuin/gopher-lua"
)

func Loader(L *lua.LState) int {
	api := map[string]lua.LGFunction{
		"encode": jsonEncode,
		"decode": jsonDecode,
	}
	L.Push(L.SetFuncs(L.NewTable(), api))
	return 1
}

func jsonEncode(L *lua.LState) int {
	value := L.CheckAny(1)
	data, err := json.Marshal(util.ToGoValue(value))
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LString(data))
	return 1
}

func jsonDecode(L *lua.LState) int {
	str := L.CheckString(1)
	var goValue interface{}
	if err := json.Unmarshal([]byte(str), &goValue); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(util.ToLuaValue(goValue))
	return 1
}
