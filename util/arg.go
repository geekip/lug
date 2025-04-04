package util

import (
	"fmt"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func ArgLString(L *lua.LState, key string, v lua.LValue) (string, bool) {
	if val, ok := v.(lua.LString); ok {
		return val.String(), true
	}
	L.ArgError(1, fmt.Sprintf("%s must be a string", key))
	return "", false
}

func ArgLInt(L *lua.LState, key string, v lua.LValue) (int, bool) {
	if val, ok := v.(lua.LNumber); ok {
		return int(val), true
	}
	L.ArgError(1, fmt.Sprintf("%s must be a number", key))
	return 0, false
}

func ArgLInt64(L *lua.LState, key string, v lua.LValue) (int64, bool) {
	if val, ok := v.(lua.LNumber); ok {
		return int64(val), true
	}
	L.ArgError(1, fmt.Sprintf("%s must be a number", key))
	return 0, false
}

func ArgLDuration(L *lua.LState, key string, v lua.LValue) (time.Duration, bool) {
	if val, ok := v.(lua.LNumber); ok {
		return time.Duration(int(val)) * time.Second, true
	}
	L.ArgError(1, fmt.Sprintf("%s must be a number (in seconds)", key))
	return 0, false
}

func ArgLTime(L *lua.LState, key string, v lua.LValue) (time.Time, bool) {
	if val, ok := v.(lua.LString); ok {
		t, err := time.Parse(time.RFC3339, val.String())
		if err != nil {
			L.ArgError(1, fmt.Sprintf("Invalid %s format: "+err.Error(), key))
		}
		return t, true
	}
	L.ArgError(1, fmt.Sprintf("%s must be a string (in time RFC3339)", key))
	return time.Time{}, false
}

func ArgLFunction(L *lua.LState, key string, v lua.LValue) (*lua.LFunction, bool) {
	if val, ok := v.(*lua.LFunction); ok {
		return val, true
	}
	L.ArgError(1, fmt.Sprintf("%s must be a function", key))
	return nil, false
}

func ArgLBool(L *lua.LState, key string, v lua.LValue) (bool, bool) {
	if val, ok := v.(lua.LBool); ok {
		return bool(val), true
	}
	L.ArgError(1, fmt.Sprintf("%s must be a function", key))
	return false, false
}

func ArgLTable(L *lua.LState, key string, v lua.LValue) ([]string, bool) {
	if val, ok := v.(*lua.LTable); ok {
		var result []string
		val.ForEach(func(_, lv lua.LValue) {
			if str, ok := lv.(lua.LString); ok {
				result = append(result, str.String())
			} else {
				L.ArgError(1, fmt.Sprintf("%s table contains non-string value", key))
				return
			}
		})
		return result, true
	}
	L.ArgError(1, fmt.Sprintf("%s must be a function", key))
	return nil, false
}

func ArgLTableMap(L *lua.LState, key string, v lua.LValue) (map[string]string, bool) {
	if val, ok := v.(*lua.LTable); ok {
		var result map[string]string
		val.ForEach(func(lk, lv lua.LValue) {
			if str, ok := lv.(lua.LString); ok {
				result[lk.String()] = str.String()
			} else {
				L.ArgError(1, fmt.Sprintf("%s table contains non-string value", key))
				return
			}
		})
		return result, true
	}
	L.ArgError(1, fmt.Sprintf("%s must be a table", key))
	return nil, false
}
