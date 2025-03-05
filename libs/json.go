package libs

import (
	"encoding/json"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Json struct{ util.Module }

func JsonLoader(L *lua.LState) int {
	mod := &Json{Module: *util.GetModule(L)}
	api := util.LGFunctions{
		"encode": mod.jsonEncode,
		"decode": mod.jsonDecode,
	}
	return mod.Api(api)
}

func (j *Json) jsonEncode(L *lua.LState) int {
	value := L.CheckAny(1)
	data, err := json.Marshal(util.ToGoValue(value))
	if err != nil {
		return j.Error(err)
	}
	return j.Push(lua.LString(data))
}

func (j *Json) jsonDecode(L *lua.LState) int {
	str := L.CheckString(1)
	var goValue interface{}
	if err := json.Unmarshal([]byte(str), &goValue); err != nil {
		return j.Error(err)
	}
	return j.Push(util.ToLuaValue(goValue))
}
