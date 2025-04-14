package libs

import (
	"bytes"
	"lug/util"
	"text/template"

	lua "github.com/yuin/gopher-lua"
)

type Template struct{}

func TemplateLoader(L *lua.LState) int {
	instance := &Template{}
	api := util.SetMethods(L, util.Methods{
		"files":  instance.executeFiles,
		"string": instance.executeString,
	})
	return util.Push(L, api)
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
		return util.NilError(L, err)
	}
	return t.executeTemplate(L, tpl)
}

func (t *Template) executeString(L *lua.LState) int {
	str := L.CheckString(1)
	key := L.OptString(3, "")
	tpl, err := util.ParseTemplateString(str, key)
	if err != nil {
		return util.NilError(L, err)
	}
	return t.executeTemplate(L, tpl)
}

func (t *Template) executeTemplate(L *lua.LState, tpl *template.Template) int {
	var data interface{}
	if L.GetTop() >= 2 {
		data = util.ToGoValue(L.CheckTable(2), false)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		buf.Reset()
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(buf.String()))
}
