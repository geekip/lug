package libs

import (
	"lug/libs/http"
	"lug/libs/sql"

	lua "github.com/yuin/gopher-lua"
)

var LibPrefix = ""

var Libs = map[string]lua.LGFunction{
	"sql":      sql.Loader,
	"fs":       FsLoader,
	"http":     http.Loader,
	"json":     JsonLoader,
	"template": TemplateLoader,
	"thread":   ThreadLoader,
	"url":      UrlLoader,
	"utf8":     Uft8Loader,
}

func Preload(L *lua.LState) {
	for name, fn := range Libs {
		if LibPrefix != "" {
			name = LibPrefix + name
		}
		L.PreloadModule(name, fn)
	}
}
