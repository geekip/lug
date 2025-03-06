package util

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type vmPool struct {
	mut sync.Mutex
	vms []*lua.LState
}

func (pl *vmPool) Get() *lua.LState {
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

func (pl *vmPool) New() *lua.LState {
	return lua.NewState()
}

func (pl *vmPool) Put(L *lua.LState) {
	pl.mut.Lock()
	defer pl.mut.Unlock()
	pl.vms = append(pl.vms, L)
}

func (pl *vmPool) Shutdown() {
	for _, L := range pl.vms {
		L.Close()
	}
}

func (pl *vmPool) Size() int {
	return len(pl.vms)
}

var VmPool = &vmPool{
	vms: make([]*lua.LState, 0),
}
