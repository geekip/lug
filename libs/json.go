package libs

import (
	"encoding/json"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Json struct{ *util.Module }

func JsonLoader(L *lua.LState) int {
	mod := &Json{
		Module: util.NewModule(L, nil),
	}
	mod.SetMethods(util.Methods{
		"encode": mod.Encode,
		"decode": mod.Decode,
	})
	return mod.Self()
}

func (j *Json) Encode(L *lua.LState) int {
	value := L.CheckAny(1)
	data, err := json.Marshal(util.ToGoValue(value, true))
	if err != nil {
		return j.NilError(err)
	}
	return j.Push(lua.LString(data))
}

func (j *Json) Decode(L *lua.LState) int {
	str := L.CheckString(1)
	var goValue interface{}
	if err := json.Unmarshal([]byte(str), &goValue); err != nil {
		return j.NilError(err)
	}
	return j.Push(util.ToLuaValue(goValue))
}
