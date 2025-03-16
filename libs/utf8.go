package libs

import (
	"lug/util"
	"unicode/utf8"
	"unsafe"

	lua "github.com/yuin/gopher-lua"
)

type Utf8 struct{ *util.Module }

var Utf8charpattern = lua.LString([]byte{
	'[', 0, '-', 0x7f, 0xc2, '-', 0xf4, ']',
	'[', 0x80, '-', 0xbf, ']', '*',
})

func Uft8Loader(L *lua.LState) int {
	mod := &Utf8{
		Module: util.NewModule(L),
	}

	mod.SetMethods(util.Methods{
		"charpattern": Utf8charpattern,
		"char":        mod.char,
		"codes":       mod.codes,
		"codepoint":   mod.codepoint,
		"len":         mod.len,
		"offset":      mod.offset,
	})

	return mod.Self()
}

func (u *Utf8) char(ls *lua.LState) int {
	args := ls.GetTop()
	b := make([]byte, 0, args)
	for i := 1; i <= args; i += 1 {
		r := rune(ls.CheckInt(i))
		if r > '\U0010FFFF' {
			ls.RaiseError("value out of range")
		}
		b = utf8.AppendRune(b, r)
	}
	char := unsafe.String(unsafe.SliceData(b), len(b))
	return u.Push(lua.LString(char))
}

func (u *Utf8) iter(ls *lua.LState) int {
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
	return u.Push(lua.LNumber(n+1), lua.LNumber(r))
}

func (u *Utf8) codes(ls *lua.LState) int {
	s := lua.LString(ls.CheckString(1))
	iter := ls.NewFunction(u.iter)
	n := lua.LNumber(0)
	return u.Push(iter, s, n)
}

func (u *Utf8) codepoint(ls *lua.LState) int {
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

func (u *Utf8) len(ls *lua.LState) int {
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
			return u.Push(lua.LNumber(l))
		}
		l += 1
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError {
			return u.Push(lua.LFalse, lua.LNumber(i+1))
		}
		i += size
	}
}

func (u *Utf8) offset(ls *lua.LState) int {
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
		return u.Push(lua.LNumber(i + 1))
	} else if i < len(s) && !utf8.RuneStart(s[i]) {
		ls.RaiseError("initial position is a continuation byte")
	}

	if n > 0 {
		n -= 1
		for {
			if n == 0 {
				return u.Push(lua.LNumber(i + 1))
			}
			if i >= len(s) {
				break
			}
			n -= 1
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError {
				return u.Push(lua.LFalse, lua.LNumber(i+1))
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
				return u.Push(lua.LFalse, lua.LNumber(i+1))
			}
			i -= size
			if n == 0 {
				return u.Push(lua.LNumber(i + 1))
			}
		}
	}
	return u.Push(lua.LNil)
}
