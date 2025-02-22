package main

import (
	"encoding/json"
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

func jsonLoader(L *lua.LState) int {
	api := map[string]lua.LGFunction{
		"encode": jsonEncode,
		"decode": jsonDecode,
	}
	L.Push(L.SetFuncs(L.NewTable(), api))
	return 1
}

func jsonEncode(L *lua.LState) int {
	value := L.CheckAny(1)
	data, err := json.Marshal(toGoMap(value))
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
	luaValue := toLuaValue(L, goValue)
	L.Push(luaValue)
	return 1
}

func toGoMap(luaValue lua.LValue) interface{} {
	switch v := luaValue.(type) {
	case *lua.LTable:
		maxIndex := v.MaxN()
		// table
		if maxIndex == 0 {
			ret := make(map[string]interface{})
			v.ForEach(func(key, value lua.LValue) {
				keystr := fmt.Sprint(toGoMap(key))
				ret[keystr] = toGoMap(value)
			})
			return ret
		}
		// array
		ret := make([]interface{}, 0, maxIndex)
		for i := 1; i <= maxIndex; i++ {
			ret = append(ret, toGoMap(v.RawGetInt(i)))
		}
		return ret
	case lua.LString:
		return string(v)
	case lua.LNumber:
		return float64(v)
	case lua.LBool:
		return bool(v)
	case *lua.LNilType:
		return nil
	default:
		return v.String()
	}
}

func toLuaValue(L *lua.LState, goValue interface{}) lua.LValue {
	switch v := goValue.(type) {
	case map[string]interface{}:
		tbl := L.NewTable()
		for key, val := range v {
			tbl.RawSetString(key, toLuaValue(L, val))
		}
		return tbl
	case []interface{}:
		tbl := L.NewTable()
		for i, val := range v {
			tbl.RawSetInt(i+1, toLuaValue(L, val))
		}
		return tbl
	case float64:
		return lua.LNumber(v)
	case string:
		return lua.LString(v)
	case bool:
		return lua.LBool(v)
	default:
		return lua.LNil
	}
}
