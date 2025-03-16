package libs

import (
	"bytes"
	"sync"
	"text/template"

	"lug/util"

	lua "github.com/yuin/gopher-lua"
)

type Template struct{ *util.Module }

type templateEntry struct {
	once sync.Once
	tmpl *template.Template
	err  error
}

var templateCache sync.Map

func TemplateLoader(L *lua.LState) int {
	mod := &Template{
		Module: util.NewModule(L),
	}
	mod.SetMethods(util.Methods{
		"file":   mod.executeFile,
		"string": mod.executeString,
	})
	return mod.Self()
}

func (t *Template) executeFile(L *lua.LState) int {
	path := L.CheckString(1)

	entryInterface, _ := templateCache.LoadOrStore(path, &templateEntry{})
	entry := entryInterface.(*templateEntry)

	entry.once.Do(func() {
		entry.tmpl, entry.err = template.ParseFiles(path)
	})

	if entry.err != nil {
		return t.NilError(entry.err)
	}
	return t.executeTemplate(entry.tmpl)
}

func (t *Template) executeString(L *lua.LState) int {
	tmplStr := L.CheckString(1)
	tmpl, err := template.New("T").Parse(tmplStr)
	if err != nil {
		return t.NilError(err)
	}
	return t.executeTemplate(tmpl)
}

func (t *Template) executeTemplate(tmpl *template.Template) int {
	var data interface{}
	if t.Vm.GetTop() >= 2 {
		data = util.ToGoValue(t.Vm.CheckTable(2), false)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return t.NilError(err)
	}
	return t.Push(lua.LString(buf.String()))
}
