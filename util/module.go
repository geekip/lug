package util

import (
	lua "github.com/yuin/gopher-lua"
)

type LGFunctions map[string]lua.LGFunction

type Module struct {
	Prototype *lua.LTable
	VmState   *lua.LState
}

func GetModule(L *lua.LState) Module {
	return Module{
		Prototype: L.NewTable(),
		VmState:   L,
	}
}

func (m *Module) This(args ...lua.LValue) int {
	m.VmState.Push(m.Prototype)
	return m.Push(args...) + 1
}

func (m *Module) Api(api LGFunctions, args ...lua.LValue) int {
	m.VmState.SetFuncs(m.Prototype, api)
	return m.This(args...)
}

func (m *Module) Push(args ...lua.LValue) int {
	aLen := len(args)
	for i := 0; i < aLen; i++ {
		m.VmState.Push(args[i])
	}
	return aLen
}
