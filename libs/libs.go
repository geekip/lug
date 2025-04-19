package libs

import (
	lhttp "lug/libs/http"
	"lug/libs/sql"

	lua "github.com/yuin/gopher-lua"
)

var LibPrefix = ""

var Libs = map[string]lua.LGFunction{
	"sql":       sql.Loader,
	"fs":        FsLoader,
	"http":      lhttp.Loader,
	"json":      JsonLoader,
	"template":  TemplateLoader,
	"waitGroup": WaitGroupLoader,
	"url":       UrlLoader,
	"utf8":      Uft8Loader,
}

func Preload(L *lua.LState) {
	for name, fn := range Libs {
		if LibPrefix != "" {
			name = LibPrefix + name
		}
		L.PreloadModule(name, fn)
	}
}
