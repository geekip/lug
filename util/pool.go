package util

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type Pool struct {
	mut sync.Mutex
	vms []*lua.LState
}

var VmPool = &Pool{
	vms: make([]*lua.LState, 0),
}

func (pl *Pool) Get() *lua.LState {
	pl.mut.Lock()
	defer pl.mut.Unlock()
	n := len(pl.vms)
	if n == 0 {
		return pl.New()
	}
	x := pl.vms[n-1]
	pl.vms = pl.vms[0 : n-1]
	return x
}

func (pl *Pool) New() *lua.LState {
	return lua.NewState()
}

func (pl *Pool) Put(L *lua.LState) {
	pl.mut.Lock()
	defer pl.mut.Unlock()
	pl.vms = append(pl.vms, L)
}

func (pl *Pool) Shutdown() {
	pl.mut.Lock()
	defer pl.mut.Unlock()

	for _, vm := range pl.vms {
		vm.Close()
	}
}

func (pl *Pool) Size() int {
	return len(pl.vms)
}
