package libs

import (
	"fmt"
	"sync"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type waitGroup struct {
	wg  *sync.WaitGroup
	api *lua.LTable
}

func WaitGroupLoader(L *lua.LState) int {
	api := util.SetMethods(L, util.Methods{
		"new": newWaitGroup,
	})
	return util.Push(L, api)
}

func newWaitGroup(L *lua.LState) int {
	instance := &waitGroup{
		wg: &sync.WaitGroup{},
	}
	api := util.SetMethods(L, util.Methods{
		"wait": instance.wait,
		"go":   instance.Go,
	})
	instance.api = api
	return util.Push(L, api)
}

func (m *waitGroup) Go(L *lua.LState) int {
	callback := L.CheckFunction(1)
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()

		vm := util.VmPool.Clone(L)
		defer util.VmPool.Put(vm)

		if err := util.CallLua(vm, callback); err != nil {
			fmt.Println(err)
			return
		}
	}()
	return util.Push(L, m.api)
}

func (m *waitGroup) wait(L *lua.LState) int {
	m.wg.Wait()
	return util.Push(L, m.api)
}
