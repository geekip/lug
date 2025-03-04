package main

import (
	"github.com/geekip/lug/db"
	"github.com/geekip/lug/fs"
	"github.com/geekip/lug/http"
	"github.com/geekip/lug/json"
	"github.com/geekip/lug/template"
	"github.com/geekip/lug/thread"
	"github.com/geekip/lug/url"
	"github.com/geekip/lug/utf8"
	lua "github.com/yuin/gopher-lua"
)

var LibPrefix = ""

var Libs = map[string]lua.LGFunction{
	"db":       db.Loader,
	"fs":       fs.Loader,
	"http":     http.Loader,
	"json":     json.Loader,
	"template": template.Loader,
	"thread":   thread.Loader,
	"url":      url.Loader,
	"utf8":     utf8.Loader,
}

func Preload(L *lua.LState) {
	for name, fn := range Libs {
		if LibPrefix != "" {
			name = LibPrefix + name
		}
		L.PreloadModule(name, fn)
	}
}
