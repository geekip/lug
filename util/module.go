package util

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

type (
	Methods map[string]interface{}
	Module  struct {
		Method *lua.LTable
		Vm     *lua.LState
	}
)

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
	m.Method = SetMethods(m.Vm, m.Method, methods...)
	return m
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
