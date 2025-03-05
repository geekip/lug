package libs

import (
	"bytes"
	"sync"
	"text/template"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Template struct{ util.Module }

type templateEntry struct {
	once sync.Once
	tmpl *template.Template
	err  error
}

var templateCache sync.Map

func TemplateLoader(L *lua.LState) int {
	mod := &Template{
		Module: *util.GetModule(L),
	}
	api := util.LGFunctions{
		"file":   mod.apiRenderFile,
		"string": mod.apiRenderString,
	}
	return mod.Api(api)
}

func (t *Template) apiRenderFile(L *lua.LState) int {
	path := L.CheckString(1)

	entryInterface, _ := templateCache.LoadOrStore(path, &templateEntry{})
	entry := entryInterface.(*templateEntry)

	entry.once.Do(func() {
		entry.tmpl, entry.err = template.ParseFiles(path)
	})

	if entry.err != nil {
		return t.Error(entry.err)
	}
	return t.renderTemplate(entry.tmpl)
}

func (t *Template) apiRenderString(L *lua.LState) int {
	tmplStr := L.CheckString(1)
	tmpl, err := template.New("T").Parse(tmplStr)
	if err != nil {
		return t.Error(err)
	}
	return t.renderTemplate(tmpl)
}

func (t *Template) renderTemplate(tmpl *template.Template) int {
	var data interface{}
	if t.VmState.GetTop() >= 2 {
		data = util.ToGoValue(t.VmState.CheckTable(2))
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return t.Error(err)
	}

	return t.Push(lua.LString(buf.String()))
}
