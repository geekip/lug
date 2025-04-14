package libs

import (
	"encoding/json"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Json struct{}

func JsonLoader(L *lua.LState) int {
	instance := &Json{}
	api := util.SetMethods(L, util.Methods{
		"encode": instance.Encode,
		"decode": instance.Decode,
	})
	return util.Push(L, api)
}

func (j *Json) Encode(L *lua.LState) int {
	value := L.CheckAny(1)
	data, err := json.Marshal(util.ToGoValue(value, true))
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(data))
}

func (j *Json) Decode(L *lua.LState) int {
	str := L.CheckString(1)
	var goValue interface{}
	if err := json.Unmarshal([]byte(str), &goValue); err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, util.ToLuaValue(goValue))
}
