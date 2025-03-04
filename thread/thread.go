package thread

import (
	"fmt"
	"sync"

	"github.com/geekip/lug/util"
	lua "github.com/yuin/gopher-lua"
)

type (
	module struct {
		util.Module
		wg *sync.WaitGroup
	}
)

func Loader(L *lua.LState) int {
	mod := util.GetModule(L)
	api := util.LGFunctions{"new": new}
	return mod.Api(api)
}

func new(L *lua.LState) int {
	mod := &module{
		util.GetModule(L),
		&sync.WaitGroup{},
	}

	api := util.LGFunctions{
		"wait": mod.wait,
		"run":  mod.run,
	}

	return mod.Api(api)
}

func (m *module) run(L *lua.LState) int {
	callback := L.CheckFunction(1)
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()

		thread := lua.NewState()
		defer thread.Close()

		err := util.CallLua(thread, callback)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
	}()
	return m.This()
}

func (m *module) wait(L *lua.LState) int {
	m.wg.Wait()
	return m.This()
}
