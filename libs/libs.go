package libs

import (
	"lug/libs/server"
	"lug/libs/sql"

	lua "github.com/yuin/gopher-lua"
)

var libPrefix = ""
var libModules = map[string]lua.LGFunction{
	"fs":        FsLoader,
	"json":      JsonLoader,
	"request":   RequestLoader,
	"server":    server.Loader,
	"sql":       sql.Loader,
	"template":  TemplateLoader,
	"url":       UrlLoader,
	"utf8":      Uft8Loader,
	"waitGroup": WaitGroupLoader,
}

func Preload(L *lua.LState) {
	for name, module := range libModules {
		if libPrefix != "" {
			name = libPrefix + name
		}
		L.PreloadModule(name, module)
	}
}
