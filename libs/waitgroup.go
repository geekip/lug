package libs

import (
	"fmt"
	"sync"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type waitGroup struct {
	*util.Module
	wg *sync.WaitGroup
}

func WaitGroupLoader(L *lua.LState) int {
	mod := util.NewModule(L, util.Methods{
		"new": newWaitGroup,
	})
	return mod.Self()
}

func newWaitGroup(L *lua.LState) int {
	mod := &waitGroup{
		Module: util.NewModule(L),
		wg:     &sync.WaitGroup{},
	}
	mod.SetMethods(util.Methods{
		"wait": mod.wait,
		"go":   mod.Go,
	})
	return mod.Self()
}

func (m *waitGroup) Go(L *lua.LState) int {
	callback := L.CheckFunction(1)
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()

		vm := util.VmPool.Get()
		defer util.VmPool.Put(vm)

		if err := util.CallLua(vm, callback); err != nil {
			fmt.Println(err)
			return
		}
	}()
	return m.Self()
}

func (m *waitGroup) wait(L *lua.LState) int {
	m.wg.Wait()
	return m.Self()
}
