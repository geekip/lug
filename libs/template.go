package libs

import (
	"bytes"
	"lug/util"
	"text/template"

	lua "github.com/yuin/gopher-lua"
)

type Template struct{ *util.Module }

func TemplateLoader(L *lua.LState) int {
	mod := &Template{
		Module: util.NewModule(L),
	}
	mod.SetMethods(util.Methods{
		"files":  mod.executeFiles,
		"string": mod.executeString,
	})
	return mod.Self()
}

func (t *Template) executeFiles(L *lua.LState) int {
	var tpl *template.Template
	var err error

	switch lpath := L.CheckAny(1).(type) {
	case lua.LString:
		tpl, err = util.ParseTemplateFiles(lpath.String())

	case *lua.LTable:
		paths := make([]string, lpath.Len())
		lpath.ForEach(func(lk, lv lua.LValue) {
			key, ok := lk.(lua.LNumber)
			if !ok {
				L.ArgError(1, "paths table must be a array table")
			}
			if str, ok := lv.(lua.LString); ok {
				paths[int(key)-1] = str.String()
			} else {
				L.ArgError(1, "paths table contains non-string value")
			}
		})
		tpl, err = util.ParseTemplateFiles(paths...)

	default:
		L.ArgError(1, "paths must be a string or array table")
	}

	if err != nil {
		return t.NilError(err)
	}
	return t.executeTemplate(tpl)
}

func (t *Template) executeString(L *lua.LState) int {
	str := L.CheckString(1)
	key := L.OptString(3, "")
	tpl, err := util.ParseTemplateString(str, key)
	if err != nil {
		return t.NilError(err)
	}
	return t.executeTemplate(tpl)
}

func (t *Template) executeTemplate(tpl *template.Template) int {
	var data interface{}
	if t.Vm.GetTop() >= 2 {
		data = util.ToGoValue(t.Vm.CheckTable(2), false)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		buf.Reset()
		return t.NilError(err)
	}
	return t.Push(lua.LString(buf.String()))
}
