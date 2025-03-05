package util

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

type LGFunctions map[string]lua.LGFunction

type Module struct {
	Prototype *lua.LTable
	VmState   *lua.LState
}

func GetModule(L *lua.LState) *Module {
	return &Module{
		Prototype: L.NewTable(),
		VmState:   L,
	}
}

func (m *Module) Self(args ...lua.LValue) int {
	m.VmState.Push(m.Prototype)
	return m.Push(args...) + 1
}

func (m *Module) Api(api LGFunctions, args ...lua.LValue) int {
	m.VmState.SetFuncs(m.Prototype, api)
	return m.Self(args...)
}

func (m *Module) Push(args ...lua.LValue) int {
	Len := len(args)
	for i := 0; i < Len; i++ {
		m.VmState.Push(args[i])
	}
	return Len
}

func (m *Module) Error(err error) int {
	m.VmState.Push(lua.LNil)
	m.VmState.Push(lua.LString(err.Error()))
	return 2
}

func (m *Module) Errorf(format string, a ...any) int {
	err := fmt.Errorf(format, a...)
	return m.Error(err)
}
