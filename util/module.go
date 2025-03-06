package util

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

type LGFunctions map[string]lua.LGFunction
type LGValues map[string]interface{}
type LValues map[string]lua.LValue

type Module struct {
	Fn *lua.LTable
	Vm *lua.LState
}

func GetModule(L *lua.LState) *Module {
	return &Module{
		Fn: L.NewTable(),
		Vm: L,
	}
}

func (m *Module) Self(args ...lua.LValue) int {
	m.Vm.Push(m.Fn)
	return m.Push(args...) + 1
}

func (m *Module) SetField(key string, val lua.LValue) *Module {
	m.Vm.SetField(m.Fn, key, val)
	return m
}

func (m *Module) SetFuncs(api LGFunctions, args ...lua.LValue) int {
	m.Vm.SetFuncs(m.Fn, api)
	return m.Self(args...)
}

func (m *Module) SetValues(apis LValues, args ...lua.LValue) int {
	for name, value := range apis {
		m.Vm.SetField(m.Fn, name, value)
	}
	return m.Self(args...)
}

func (m *Module) SetAnys(apis LGValues, args ...lua.LValue) int {
	for name, value := range apis {
		switch v := value.(type) {
		case lua.LGFunction:
			m.Vm.SetField(m.Fn, name, m.Vm.NewFunction(v))
		default:
			m.Vm.SetField(m.Fn, name, ToLuaValue(v))
		}
	}
	return m.Self(args...)
}

func (m *Module) Push(args ...lua.LValue) int {
	Len := len(args)
	for i := 0; i < Len; i++ {
		m.Vm.Push(args[i])
	}
	return Len
}

func (m *Module) Error(err error) int {
	m.Vm.Push(lua.LNil)
	m.Vm.Push(lua.LString(err.Error()))
	return 2
}

func (m *Module) Errorf(format string, a ...any) int {
	err := fmt.Errorf(format, a...)
	return m.Error(err)
}
