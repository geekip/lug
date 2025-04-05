package util

import (
	"fmt"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func getIndex(n []int) int {
	if len(n) > 0 {
		return n[0]
	}
	return 1
}

func CheckString(L *lua.LState, key string, v lua.LValue, n ...int) (string, bool) {
	if val, ok := v.(lua.LString); ok {
		return val.String(), true
	}
	i := getIndex(n)
	L.ArgError(i, fmt.Sprintf("%s must be a string", key))
	return "", false
}

func CheckInt(L *lua.LState, key string, v lua.LValue, n ...int) (int, bool) {
	if val, ok := v.(lua.LNumber); ok {
		return int(val), true
	}
	i := getIndex(n)
	L.ArgError(i, fmt.Sprintf("%s must be a number", key))
	return 0, false
}

func CheckInt64(L *lua.LState, key string, v lua.LValue, n ...int) (int64, bool) {
	if val, ok := v.(lua.LNumber); ok {
		return int64(val), true
	}
	i := getIndex(n)
	L.ArgError(i, fmt.Sprintf("%s must be a number", key))
	return 0, false
}

func CheckDuration(L *lua.LState, key string, v lua.LValue, n ...int) (time.Duration, bool) {
	if val, ok := v.(lua.LNumber); ok {
		return time.Duration(int(val)) * time.Second, true
	}
	i := getIndex(n)
	L.ArgError(i, fmt.Sprintf("%s must be a number (in seconds)", key))
	return 0, false
}

func CheckTime(L *lua.LState, key string, v lua.LValue, n ...int) (time.Time, bool) {
	i := getIndex(n)
	if val, ok := v.(lua.LString); ok {
		t, err := time.Parse(time.RFC3339, val.String())
		if err != nil {
			L.ArgError(i, fmt.Sprintf("Invalid %s format: %s", key, err.Error()))
			return time.Time{}, false
		}
		return t, true
	}
	L.ArgError(i, fmt.Sprintf("%s must be a string (RFC3339 format)", key))
	return time.Time{}, false
}

func CheckFunction(L *lua.LState, key string, v lua.LValue, n ...int) (*lua.LFunction, bool) {
	if val, ok := v.(*lua.LFunction); ok {
		return val, true
	}
	i := getIndex(n)
	L.ArgError(i, fmt.Sprintf("%s must be a function", key))
	return nil, false
}

func CheckBool(L *lua.LState, key string, v lua.LValue, n ...int) (bool, bool) {
	if val, ok := v.(lua.LBool); ok {
		return bool(val), true
	}
	i := getIndex(n)
	L.ArgError(i, fmt.Sprintf("%s must be a boolean", key))
	return false, false
}

func CheckTable(L *lua.LState, key string, v lua.LValue, n ...int) ([]string, bool) {
	i := getIndex(n)
	if val, ok := v.(*lua.LTable); ok {

		maxIndex := val.MaxN()
		if maxIndex == 1 {
			maxn := val.Len()
			result := make([]string, maxn)
			for idx := 1; idx <= maxn; idx++ {
				lv := val.RawGetInt(idx)
				if str, ok := lv.(lua.LString); ok {
					result = append(result, str.String())
				} else {
					L.ArgError(i, fmt.Sprintf("%s table contains non-string value at index %d", key, idx))
					return nil, false
				}
			}
			return result, true
		} else {
			L.ArgError(i, fmt.Sprintf("%s must be a table (array)", key))
		}
	}
	L.ArgError(i, fmt.Sprintf("%s must be a table", key))
	return nil, false
}

func CheckTableMap(L *lua.LState, key string, v lua.LValue, n ...int) (map[string]string, bool) {
	i := getIndex(n)
	if val, ok := v.(*lua.LTable); ok {
		result := make(map[string]string, val.Len())
		val.ForEach(func(lk, lv lua.LValue) {
			if str, ok := lv.(lua.LString); ok {
				result[lk.String()] = str.String()
			} else {
				L.ArgError(i, fmt.Sprintf("%s table contains non-string value", key))
				return
			}
		})
		return result, true
	}
	L.ArgError(i, fmt.Sprintf("%s must be a table", key))
	return nil, false
}
