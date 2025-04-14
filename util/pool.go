package util

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type Pool struct {
	mut sync.Mutex
	vms []*lua.LState
	max int
}

var VmPool = &Pool{
	vms: make([]*lua.LState, 0, 10),
	max: 20,
}

var excludes = map[string]bool{
	// "arg":              true,
	// "package":          true,
	"_G":                  true,
	"_VERSION":            true,
	"_GOPHER_LUA_VERSION": true,
	"loadfile":            true,
	"xpcall":              true,
	"getfenv":             true,
	"getmetatable":        true,
	"load":                true,
	"require":             true,
	"assert":              true,
	"collectgarbage":      true,
	"next":                true,
	"print":               true,
	"rawset":              true,
	"tonumber":            true,
	"error":               true,
	"loadstring":          true,
	"rawequal":            true,
	"module":              true,
	"dofile":              true,
	"setmetatable":        true,
	"type":                true,
	"newproxy":            true,
	"select":              true,
	"_printregs":          true,
	"setfenv":             true,
	"tostring":            true,
	"unpack":              true,
	"pcall":               true,
	"rawget":              true,
	"ipairs":              true,
	"pairs":               true,
	"table":               true,
	"io":                  true,
	"os":                  true,
	"string":              true,
	"math":                true,
	"debug":               true,
	"channel":             true,
	"coroutine":           true,
}

func (p *Pool) Get() *lua.LState {
	p.mut.Lock()
	defer p.mut.Unlock()
	n := len(p.vms)
	if n > 0 {
		vm := p.vms[n-1]
		p.vms = p.vms[:n-1]
		return vm
	}
	return p.New()
}

func (p *Pool) New() *lua.LState {
	return lua.NewState()
}

func (p *Pool) Clone(L *lua.LState) *lua.LState {
	vm := p.Get()
	L.G.Global.ForEach(func(k, v lua.LValue) {
		if !excludes[k.String()] {
			vm.G.Global.RawSet(k, v)
		}
	})
	vm.SetTop(0) // Ensure the stack is clean
	return vm
}

func (p *Pool) Put(L *lua.LState) {
	p.mut.Lock()
	defer p.mut.Unlock()
	if len(p.vms) >= p.max {
		L.Close()
	} else {
		L.SetTop(0)
		p.vms = append(p.vms, L)
	}
}

func (p *Pool) Shutdown() {
	p.mut.Lock()
	defer p.mut.Unlock()
	for _, vm := range p.vms {
		vm.Close()
	}
	p.vms = nil // 帮助GC回收
}

func (p *Pool) Size() int {
	p.mut.Lock()
	defer p.mut.Unlock()
	return len(p.vms)
}
