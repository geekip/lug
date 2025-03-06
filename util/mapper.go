package util

import (
	"math"
	"reflect"

	lua "github.com/yuin/gopher-lua"
)

func ToLuaValue(gv interface{}) lua.LValue {
	switch v := gv.(type) {
	case lua.LValue:
		return v
	case bool:
		return lua.LBool(v)
	case string:
		return lua.LString(v)
	case float64:
		return lua.LNumber(v)
	case int:
		return lua.LNumber(float64(v))
	case int64:
		return lua.LNumber(float64(v))
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
	}

	v := reflect.ValueOf(gv)
	switch v.Kind() {
	case reflect.Float32:
		return lua.LNumber(v.Float())
	case reflect.Slice, reflect.Array:
		arr := &lua.LTable{}
		length := v.Len()
		for i := 0; i < length; i++ {
			arr.RawSetInt(i+1, ToLuaValue(v.Index(i).Interface()))
		}
		return arr
	case reflect.Map:
		obj := &lua.LTable{}
		iter := v.MapRange()
		for iter.Next() {
			obj.RawSetH(
				ToLuaValue(iter.Key().Interface()),
				ToLuaValue(iter.Value().Interface()),
			)
		}
		return obj
	}
	return lua.LNil
}

func ToGoValue(lv lua.LValue, likeJson bool) interface{} {
	switch v := lv.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(v)
	case lua.LString:
		return string(v)
	case lua.LNumber:
		num := float64(v)
		intNum := int64(num)
		if num == float64(intNum) {
			if intNum >= math.MinInt && intNum <= math.MaxInt {
				return int(intNum)
			}
			return intNum
		}
		return num
	case *lua.LTable:
		maxIndex := v.MaxN()
		if maxIndex == 0 {
			if likeJson {
				obj := make(map[string]interface{})
				v.ForEach(func(key, value lua.LValue) {
					obj[key.String()] = ToGoValue(value, likeJson)
				})
				return obj
			}

			obj := make(map[interface{}]interface{})
			v.ForEach(func(key, value lua.LValue) {
				obj[ToGoValue(key, likeJson)] = ToGoValue(value, likeJson)
			})
			return obj
		}

		arr := make([]interface{}, 0, maxIndex)
		for i := 1; i <= maxIndex; i++ {
			arr = append(arr, ToGoValue(v.RawGetInt(i), likeJson))
		}

		// Check if there are any additional keys
		hasExtra := false
		v.ForEach(func(key, _ lua.LValue) {
			if key.Type() != lua.LTNumber {
				hasExtra = true
				return
			}
			n := key.(lua.LNumber)
			if n != lua.LNumber(int(n)) || n < 1 || n > lua.LNumber(maxIndex) {
				hasExtra = true
			}
		})
		if !hasExtra {
			return arr
		}

		// Merge array and hash parts
		obj := make(map[interface{}]interface{})
		for i, val := range arr {
			obj[i+1] = val
		}
		v.ForEach(func(key, value lua.LValue) {
			goKey := ToGoValue(key, likeJson)
			if num, ok := goKey.(int); ok && num >= 1 && num <= maxIndex {
				return // Skip processed keys in the array section
			}
			obj[goKey] = ToGoValue(value, likeJson)
		})
		return obj
	case *lua.LUserData:
		return v.Value
	default:
		return v.String()
	}
}
