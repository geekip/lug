package libs

import (
	"fmt"
	"sync"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Thread struct {
	util.Module
	wg *sync.WaitGroup
}

func ThreadLoader(L *lua.LState) int {
	mod := util.GetModule(L)
	api := util.LGFunctions{"new": newThread}
	return mod.SetFuncs(api)
}

func newThread(L *lua.LState) int {
	mod := &Thread{
		Module: *util.GetModule(L),
		wg:     &sync.WaitGroup{},
	}
	api := util.LGFunctions{
		"wait": mod.wait,
		"run":  mod.run,
	}
	return mod.SetFuncs(api)
}

func (m *Thread) run(L *lua.LState) int {

	callback := L.CheckFunction(1)
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()

		vm := util.VmPool.Get()
		defer util.VmPool.Put(vm)

		err := util.CallLua(vm, callback)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
	}()
	return m.Self()
}

func (m *Thread) wait(L *lua.LState) int {
	m.wg.Wait()
	return m.Self()
}
