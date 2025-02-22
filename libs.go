package main

import (
	lua "github.com/yuin/gopher-lua"
)

var luaLibs = map[string]lua.LGFunction{
	"fs":       fsLoader,
	"json":     jsonLoader,
	"http":     httpLoader,
	"router":   routerLoader,
	"template": templateLoader,
}
