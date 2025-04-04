package http

import (
	lua "github.com/yuin/gopher-lua"
)

// func checkContext(L *lua.LState, n int) *Context {
// 	ud := L.CheckUserData(n)
// 	ctx, ok := ud.Value.(*Context)
// 	if !ok {
// 		L.ArgError(1, "must be http_server_context")
// 	}
// 	return ctx
// }

func Loader(L *lua.LState) int {
	// ctx := checkContext(L, 1)
	// http_server_context_ud := L.NewTypeMetatable(`http_server_context`)
	// L.SetGlobal(`http_server_context_ud`, http_server_context_ud)
	// L.SetField(http_server_context_ud, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
	// 	"code":     ctx.code,
	// 	"header":   Header,
	// 	"write":    Write,
	// 	"redirect": Redirect,
	// 	"done":     Done,
	// }))

	mod := L.NewTable()
	// mod.RawSetString("client", newClient(L))
	// mod.RawSetString("server", newServer(L))
	L.SetFuncs(mod, map[string]lua.LGFunction{
		"client": newClient,
		"server": newServer,
	})
	L.Push(mod)
	return 1
}
