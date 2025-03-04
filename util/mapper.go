package util

import (
	lua "github.com/yuin/gopher-lua"
)

func ToLuaValue(gv interface{}) lua.LValue {
	switch v := gv.(type) {
	case lua.LValue:
		return v
	case bool:
		return lua.LBool(v)
	case float64:
		return lua.LNumber(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case string:
		return lua.LString(v)
	case []interface{}:
		arr := &lua.LTable{}
		for i, val := range v {
			arr.RawSetInt(i+1, ToLuaValue(val))
		}
		return arr
	case map[string]interface{}:
		obj := &lua.LTable{}
		for key, val := range v {
			obj.RawSetString(key, ToLuaValue(val))
		}
		return obj
	case map[interface{}]interface{}:
		obj := &lua.LTable{}
		for key, val := range v {
			obj.RawSetH(ToLuaValue(key), ToLuaValue(val))
		}
		return obj
	case interface{}:
		if v, ok := v.(bool); ok {
			return lua.LBool(v)
		}
		if v, ok := v.(float64); ok {
			return lua.LNumber(v)
		}
		if v, ok := v.(string); ok {
			return lua.LString(v)
		}
	}
	return lua.LNil
}

func ToGoValue(lv lua.LValue) interface{} {
	switch v := lv.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return lua.LVAsBool(v)
	case lua.LString:
		return lua.LVAsString(v)
	case lua.LNumber:
		num := float64(lua.LVAsNumber(v))
		intNum := int64(num)
		if num != float64(intNum) {
			return num
		}
		return intNum
	case *lua.LTable:
		maxIndex := v.MaxN()
		// table
		if maxIndex == 0 {
			obj := make(map[string]interface{})
			v.ForEach(func(key, value lua.LValue) {
				obj[lua.LVAsString(key)] = ToGoValue(value)
			})
			return obj
		}
		// array
		arr := make([]interface{}, 0, maxIndex)
		for i := 1; i <= maxIndex; i++ {
			arr = append(arr, ToGoValue(v.RawGetInt(i)))
		}
		return arr
	default:
		return v.String()
	}
}
