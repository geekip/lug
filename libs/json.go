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
		"encode": mod.Encode,
		"decode": mod.Decode,
	}
	return mod.SetFuncs(api)
}

func (j *Json) Encode(L *lua.LState) int {
	value := L.CheckAny(1)
	data, err := json.Marshal(util.ToGoValue(value, true))
	if err != nil {
		return j.Error(err)
	}
	return j.Push(lua.LString(data))
}

func (j *Json) Decode(L *lua.LState) int {
	str := L.CheckString(1)
	var goValue interface{}
	if err := json.Unmarshal([]byte(str), &goValue); err != nil {
		return j.Error(err)
	}
	return j.Push(util.ToLuaValue(goValue))
}
