package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

var fileLocks sync.Map

func fsLoader(L *lua.LState) int {
	api := map[string]lua.LGFunction{
		"mkdir":    mkdir,
		"copy":     copyFile,
		"chmod":    chmod,
		"move":     moveFile,
		"remove":   remove,
		"read":     read,
		"write":    write,
		"isdir":    isdir,
		"dirname":  dirname,
		"basename": basename,
		"exedir":   exedir,
		"cwdir":    cwdir,
		"symlink":  symlink,
		"ext":      ext,
		"exists":   exists,
		"glob":     glob,
		"join":     join,
		"clean":    clean,
		"abspath":  abspath,
		"isabs":    isabs,
	}
	L.Push(L.SetFuncs(L.NewTable(), api))
	return 1
}

func pushError(L *lua.LState, err error) int {
	L.Push(lua.LNil)
	L.Push(lua.LString(err.Error()))
	return 2
}

func mkdir(L *lua.LState) int {
	path := L.CheckString(1)
	recursive := L.OptBool(2, true)
	mode := os.FileMode(0755)

	if L.GetTop() >= 3 {
		m, err := oct2decimal(L.CheckInt(3))
		if err != nil {
			return pushError(L, err)
		}
		mode = os.FileMode(m)
	}

	if recursive {
		if err := os.MkdirAll(path, mode); err != nil {
			return pushError(L, err)
		}
	} else {
		if err := os.Mkdir(path, mode); err != nil {
			return pushError(L, err)
		}
	}

	L.Push(lua.LTrue)
	return 1
}

func remove(L *lua.LState) int {
	path := L.CheckString(1)

	if L.GetTop() >= 2 && L.ToBool(2) {
		if err := os.RemoveAll(path); err != nil {
			return pushError(L, err)
		}
	} else {
		if err := os.Remove(path); err != nil {
			return pushError(L, err)
		}
	}
	L.Push(lua.LTrue)
	return 1
}

func isdir(L *lua.LState) int {
	path := L.CheckString(1)
	stat, err := os.Stat(path)
	if err != nil {
		return pushError(L, err)
	}
	L.Push(lua.LBool(stat.IsDir()))
	return 1
}

func dirname(L *lua.LState) int {
	path := L.CheckString(1)
	L.Push(lua.LString(filepath.Dir(path)))
	return 1
}

func basename(L *lua.LState) int {
	path := L.CheckString(1)
	L.Push(lua.LString(filepath.Base(path)))
	return 1
}

func exedir(L *lua.LState) int {
	path, err := os.Executable()
	if err != nil {
		return pushError(L, err)
	}
	L.Push(lua.LString(filepath.Dir(path)))
	return 1
}

func cwdir(L *lua.LState) int {
	path, err := os.Getwd()
	if err != nil {
		return pushError(L, err)
	}
	L.Push(lua.LString(path))
	return 1
}

func symlink(L *lua.LState) int {
	target := L.CheckString(1)
	link := L.CheckString(2)
	if err := os.Symlink(target, link); err != nil {
		return pushError(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

func ext(L *lua.LState) int {
	L.Push(lua.LString(filepath.Ext(L.CheckString(1))))
	return 1
}

func exists(L *lua.LState) int {
	path := L.CheckString(1)
	_, err := os.Stat(path)
	L.Push(lua.LBool(!os.IsNotExist(err)))
	return 1
}

func read(L *lua.LState) int {
	path := L.CheckString(1)
	content, err := os.ReadFile(path)
	if err != nil {
		return pushError(L, err)
	}
	L.Push(lua.LString(content))
	return 1
}

func write(L *lua.LState) int {
	path := L.CheckString(1)
	data := L.CheckString(2)
	append := L.OptBool(3, false)
	mode := os.FileMode(0644)

	if L.GetTop() >= 4 {
		if m, err := oct2decimal(L.CheckInt(4)); err != nil {
			return pushError(L, err)
		} else {
			mode = os.FileMode(m)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), mode|0755); err != nil {
		return pushError(L, err)
	}

	mu, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	if append {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, mode)
		if err != nil {
			return pushError(L, err)
		}
		defer file.Close()

		if _, err := file.WriteString(data); err != nil {
			return pushError(L, err)
		}
	} else {
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			return pushError(L, err)
		}
	}

	L.Push(lua.LTrue)
	return 1
}

func glob(L *lua.LState) int {
	pattern := L.CheckString(1)

	files, err := filepath.Glob(pattern)
	if err != nil {
		return pushError(L, err)
	}
	result := L.CreateTable(len(files), 0)
	for _, file := range files {
		result.Append(lua.LString(file))
	}
	L.Push(result)
	return 1
}

func join(L *lua.LState) int {
	elems := make([]string, L.GetTop())
	for i := 1; i <= L.GetTop(); i++ {
		elems[i-1] = L.CheckString(i)
	}
	L.Push(lua.LString(filepath.Join(elems...)))
	return 1
}

func clean(L *lua.LState) int {
	path := L.CheckString(1)
	L.Push(lua.LString(filepath.Clean(path)))
	return 1
}

func abspath(L *lua.LState) int {
	path := L.CheckString(1)
	ret, err := filepath.Abs(path)
	if err != nil {
		return pushError(L, err)
	}
	L.Push(lua.LString(ret))
	return 1
}

func isabs(L *lua.LState) int {
	path := L.CheckString(1)
	L.Push(lua.LBool(filepath.IsAbs(path)))
	return 1
}

func copyFile(L *lua.LState) int {
	src := L.CheckString(1)
	dest := L.CheckString(2)

	sf, err := os.Open(src)
	if err != nil {
		return pushError(L, err)
	}
	defer sf.Close()

	si, err := sf.Stat()
	if err != nil {
		return pushError(L, err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return pushError(L, err)
	}

	df, err := os.Create(dest)
	if err != nil {
		return pushError(L, err)
	}
	defer df.Close()

	if _, err := io.Copy(df, sf); err != nil {
		return pushError(L, err)
	}

	if err := os.Chmod(dest, si.Mode()); err != nil {
		return pushError(L, err)
	}

	L.Push(lua.LTrue)
	return 1
}

func moveFile(L *lua.LState) int {
	src := L.CheckString(1)
	dest := L.CheckString(2)

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return pushError(L, err)
	}

	if err := os.Rename(src, dest); err != nil {
		return pushError(L, err)
	}

	L.Push(lua.LTrue)
	return 1
}

func chmod(L *lua.LState) int {
	path := L.CheckString(1)
	mode, err := oct2decimal(L.CheckInt(2))
	if err != nil {
		return pushError(L, err)
	}
	if err := os.Chmod(path, os.FileMode(mode)); err != nil {
		return pushError(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

func oct2decimal(oct int) (uint64, error) {
	return strconv.ParseUint(fmt.Sprintf("%d", oct), 8, 32)
}
