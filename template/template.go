package template

import (
	"bytes"
	"sync"
	"text/template"

	"github.com/geekip/lug/util"
	lua "github.com/yuin/gopher-lua"
)

type templateEntry struct {
	once sync.Once
	tmpl *template.Template
	err  error
}

var templateCache sync.Map

func Loader(L *lua.LState) int {
	api := map[string]lua.LGFunction{
		"file":   apiRenderFile,
		"string": apiRenderString,
	}
	L.Push(L.SetFuncs(L.NewTable(), api))
	return 1
}

func apiRenderFile(L *lua.LState) int {
	path := L.CheckString(1)

	entryInterface, _ := templateCache.LoadOrStore(path, &templateEntry{})
	entry := entryInterface.(*templateEntry)

	entry.once.Do(func() {
		entry.tmpl, entry.err = template.ParseFiles(path)
	})

	if entry.err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(entry.err.Error()))
		return 2
	}
	return renderTemplate(L, entry.tmpl)
}

func apiRenderString(L *lua.LState) int {
	tmplStr := L.CheckString(1)
	tmpl, err := template.New("T").Parse(tmplStr)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	return renderTemplate(L, tmpl)
}

func renderTemplate(L *lua.LState, tmpl *template.Template) int {
	var data interface{}
	if L.GetTop() >= 2 {
		data = util.ToGoValue(L.CheckTable(2))
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LString(buf.String()))
	return 1
}
