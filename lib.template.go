package main

import (
	"bytes"
	"fmt"
	"sync"
	"text/template"

	lua "github.com/yuin/gopher-lua"
)

type templateEntry struct {
	once sync.Once
	tmpl *template.Template
	err  error
}

var templateCache sync.Map

func templateLoader(L *lua.LState) int {
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
		data = toGoValue(L.CheckTable(2))
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

func toGoValue(lv lua.LValue) interface{} {
	switch v := lv.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(v)
	case lua.LString:
		return string(v)
	case lua.LNumber:
		return float64(v)
	case *lua.LTable:
		maxIndex := v.MaxN()
		// table
		if maxIndex == 0 {
			ret := make(map[interface{}]interface{})
			v.ForEach(func(key, value lua.LValue) {
				keystr := fmt.Sprint(toGoValue(key))
				ret[keystr] = toGoValue(value)
			})
			return ret
		}
		// array
		ret := make([]interface{}, 0, maxIndex)
		for i := 1; i <= maxIndex; i++ {
			ret = append(ret, toGoValue(v.RawGetInt(i)))
		}
		return ret
	default:
		return v
	}
}
