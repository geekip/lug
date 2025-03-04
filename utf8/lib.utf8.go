package utf8

import (
	"unicode/utf8"
	"unsafe"

	lua "github.com/yuin/gopher-lua"
)

var Utf8charpattern = lua.LString([]byte{
	'[', 0, '-', 0x7f, 0xc2, '-', 0xf4, ']',
	'[', 0x80, '-', 0xbf, ']', '*',
})

func Loader(L *lua.LState) int {
	api := map[string]lua.LGFunction{
		"char":      Utf8char,
		"codes":     Utf8codes,
		"codepoint": Utf8codepoint,
		"len":       Utf8len,
		"offset":    Utf8offset,
	}
	mod := L.SetFuncs(L.CreateTable(0, 6), api)
	mod.RawSetString("charpattern", Utf8charpattern)
	L.Push(mod)
	return 1
}

func Utf8char(ls *lua.LState) int {
	args := ls.GetTop()
	b := make([]byte, 0, args)
	for i := 1; i <= args; i += 1 {
		r := rune(ls.CheckInt(i))
		if r > '\U0010FFFF' {
			ls.RaiseError("value out of range")
		}
		b = utf8.AppendRune(b, r)
	}
	ls.Push(lua.LString(unsafe.String(unsafe.SliceData(b), len(b))))
	return 1
}

func utf8iter(ls *lua.LState) int {
	s := ls.CheckString(1)
	n := ls.CheckInt(2) - 1
	if n < 0 {
		n = 0
	} else if n < len(s) {
		for {
			n += 1
			if n == len(s) || utf8.RuneStart(s[n]) {
				break
			}
		}
	}
	if n >= len(s) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(s[n:])
	if r == utf8.RuneError {
		ls.RaiseError("invalid UTF-8 code")
	}
	ls.Push(lua.LNumber(n + 1))
	ls.Push(lua.LNumber(r))
	return 2
}

func Utf8codes(ls *lua.LState) int {
	s := ls.CheckString(1)
	ls.Push(ls.NewFunction(utf8iter))
	ls.Push(lua.LString(s))
	ls.Push(lua.LNumber(0))
	return 3
}

func Utf8codepoint(ls *lua.LState) int {
	s := ls.CheckString(1)
	i := ls.OptInt(2, 1)
	j := ls.OptInt(3, i)
	i -= 1
	if i < 0 {
		i += len(s) + 1
	}
	if j < 0 {
		j += len(s) + 1
	}
	if j <= i || i == len(s)+1 {
		return 0
	}
	if i < 0 || i > len(s) || j < 1 || j > len(s) {
		ls.RaiseError("position out of range")
	}

	n := 0
	for {
		if i >= j {
			return n
		}
		n += 1
		r, size := utf8.DecodeRuneInString(s[i:])
		ls.Push(lua.LNumber(r))
		i += size
		if r == utf8.RuneError {
			ls.RaiseError("invalid UTF-8 code")
		}
	}
}

func Utf8len(ls *lua.LState) int {
	s := ls.CheckString(1)
	i := int(ls.OptNumber(2, 1))
	j := int(ls.OptNumber(3, -1))
	if i < 0 {
		i += len(s)
	} else {
		i -= 1
	}
	if j < 0 {
		j += len(s) + 1
	}
	l := 0
	for {
		if i >= j {
			ls.Push(lua.LNumber(l))
			return 1
		}
		l += 1
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError {
			ls.Push(lua.LFalse)
			ls.Push(lua.LNumber(i + 1))
			return 2
		}
		i += size
	}
}

func Utf8offset(ls *lua.LState) int {
	s := ls.CheckString(1)
	n := ls.CheckInt(2)
	var i int
	if n < 0 {
		i = ls.OptInt(3, len(s)+1)
	} else {
		i = ls.OptInt(3, 1)
	}
	if i < 0 {
		i += len(s)
	} else {
		i -= 1
	}
	if i < 0 || i > len(s) {
		ls.RaiseError("position out of range")
	}

	if n == 0 {
		if i < len(s) {
			for i > 0 {
				if utf8.RuneStart(s[i]) {
					break
				}
				i -= 1
			}
		}
		ls.Push(lua.LNumber(i + 1))
		return 1
	} else if i < len(s) && !utf8.RuneStart(s[i]) {
		ls.RaiseError("initial position is a continuation byte")
	}

	if n > 0 {
		n -= 1
		for {
			if n == 0 {
				ls.Push(lua.LNumber(i + 1))
				return 1
			}
			if i >= len(s) {
				break
			}
			n -= 1
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError {
				ls.Push(lua.LFalse)
				ls.Push(lua.LNumber(i + 1))
				return 2
			}
			i += size
		}
	} else {
		for {
			if i <= 0 {
				break
			}
			n += 1
			r, size := utf8.DecodeLastRuneInString(s[:i])
			if r == utf8.RuneError {
				ls.Push(lua.LFalse)
				ls.Push(lua.LNumber(i + 1))
				return 2
			}
			i -= size
			if n == 0 {
				ls.Push(lua.LNumber(i + 1))
				return 1
			}
		}
	}
	ls.Push(lua.LNil)
	return 1
}
