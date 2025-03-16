package util

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

type Methods map[string]interface{}

type Module struct {
	Method *lua.LTable
	Vm     *lua.LState
}

func NewModule(L *lua.LState, methods ...Methods) *Module {
	mod := &Module{
		Method: L.NewTable(),
		Vm:     L,
	}
	return mod.SetMethods(methods...)
}

func (m *Module) SetMethods(methods ...Methods) *Module {
	if len(methods) == 0 {
		return m
	}
	for i := 0; i < len(methods); i++ {
		for key, val := range methods[i] {
			switch v := val.(type) {
			case lua.LGFunction:
				m.Method.RawSetString(key, m.Vm.NewClosure(v))
			case func(*lua.LState) int:
				m.Method.RawSetString(key, m.Vm.NewClosure(v))
			case lua.LValue:
				m.Method.RawSetString(key, v)
			default:
				lv := ToLuaValue(v)
				m.Method.RawSetString(key, lv)
				if lv == lua.LNil {
					err := fmt.Errorf("unsupported method type: %s", key)
					DebugPrintError(err)
				}
			}
		}
	}
	return m
}

func (m *Module) CallLua(callback *lua.LFunction, args ...lua.LValue) error {
	m.Vm.SetTop(0)
	m.Push(callback)
	alen := m.Push(args...)
	return m.Vm.PCall(alen, 1, nil)
}

func (m *Module) Self(args ...lua.LValue) int {
	m.Vm.Push(m.Method)
	return m.Push(args...) + 1
}

func (m *Module) Push(args ...lua.LValue) int {
	Len := len(args)
	for i := 0; i < Len; i++ {
		m.Vm.Push(args[i])
	}
	return Len
}

func (m *Module) NilError(err error) int {
	m.Vm.Push(lua.LNil)
	return m.Error(err) + 1
}

func (m *Module) Error(err error) int {
	m.Vm.Push(lua.LString(err.Error()))
	return 1
}

func (m *Module) Errorf(format string, a ...any) int {
	err := fmt.Errorf(format, a...)
	return m.Error(err)
}

func (m *Module) RaiseError(err error) int {
	m.Vm.RaiseError(err.Error(), nil)
	return 0
}
