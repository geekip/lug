package libs

import (
	"fmt"
	"io"
	"lug/util"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type Fs struct{ util.Module }

var fileLocks sync.Map

func FsLoader(L *lua.LState) int {
	mod := &Fs{
		Module: *util.GetModule(L),
	}
	api := util.LGFunctions{
		"mkdir":     mod.mkdir,
		"copy":      mod.copyFile,
		"chmod":     mod.chmod,
		"move":      mod.moveFile,
		"remove":    mod.remove,
		"read":      mod.read,
		"write":     mod.write,
		"isdir":     mod.isdir,
		"dirname":   mod.dirname,
		"basename":  mod.basename,
		"exedir":    mod.exedir,
		"cwdir":     mod.cwdir,
		"symlink":   mod.symlink,
		"ext":       mod.ext,
		"exists":    mod.exists,
		"glob":      mod.glob,
		"join":      mod.join,
		"clean":     mod.clean,
		"abspath":   mod.abspath,
		"isabs":     mod.isabs,
		"fromSlash": mod.fromSlash,
		"toSlash":   mod.toSlash,
	}
	return mod.SetFuncs(api)
}

func (f *Fs) mkdir(L *lua.LState) int {
	path := L.CheckString(1)
	recursive := L.OptBool(2, true)
	mode := os.FileMode(0755)

	if L.GetTop() >= 3 {
		m, err := oct2decimal(L.CheckInt(3))
		if err != nil {
			return f.Error(err)
		}
		mode = os.FileMode(m)
	}

	if recursive {
		if err := os.MkdirAll(path, mode); err != nil {
			return f.Error(err)
		}
	} else {
		if err := os.Mkdir(path, mode); err != nil {
			return f.Error(err)
		}
	}
	return f.Push(lua.LTrue)
}

func (f *Fs) remove(L *lua.LState) int {
	path := L.CheckString(1)
	recursive := L.OptBool(2, false)

	if recursive {
		if err := os.RemoveAll(path); err != nil {
			return f.Error(err)
		}
	} else {
		if err := os.Remove(path); err != nil {
			return f.Error(err)
		}
	}
	return f.Push(lua.LTrue)
}

func (f *Fs) isdir(L *lua.LState) int {
	path := L.CheckString(1)
	stat, err := os.Stat(path)
	if err != nil {
		return f.Error(err)
	}
	return f.Push(lua.LBool(stat.IsDir()))
}

func (f *Fs) dirname(L *lua.LState) int {
	return f._dirname(L.CheckString(1))
}

func (f *Fs) basename(L *lua.LState) int {
	path := L.CheckString(1)
	return f.Push(lua.LString(filepath.Base(path)))
}

func (f *Fs) exedir(L *lua.LState) int {
	path, err := os.Executable()
	if err != nil {
		return f.Error(err)
	}
	return f._dirname(path)
}

func (f *Fs) _dirname(path string) int {
	return f.Push(lua.LString(filepath.Dir(path)))
}

func (f *Fs) cwdir(L *lua.LState) int {
	path, err := os.Getwd()
	if err != nil {
		return f.Error(err)
	}
	return f.Push(lua.LString(path))
}

func (f *Fs) symlink(L *lua.LState) int {
	target := L.CheckString(1)
	link := L.CheckString(2)
	if err := os.Symlink(target, link); err != nil {
		return f.Error(err)
	}
	return f.Push(lua.LTrue)
}

func (f *Fs) ext(L *lua.LState) int {
	return f.Push(lua.LString(filepath.Ext(L.CheckString(1))))
}

func (f *Fs) exists(L *lua.LState) int {
	path := L.CheckString(1)
	_, err := os.Stat(path)
	return f.Push(lua.LBool(!os.IsNotExist(err)))
}

func (f *Fs) read(L *lua.LState) int {
	path := L.CheckString(1)
	content, err := os.ReadFile(path)
	if err != nil {
		return f.Error(err)
	}
	return f.Push(lua.LString(content))
}

func (f *Fs) write(L *lua.LState) int {
	path := L.CheckString(1)
	data := L.CheckString(2)
	append := L.OptBool(3, false)
	mode := os.FileMode(0644)

	if L.GetTop() >= 4 {
		if m, err := oct2decimal(L.CheckInt(4)); err != nil {
			return f.Error(err)
		} else {
			mode = os.FileMode(m)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), mode|0755); err != nil {
		return f.Error(err)
	}

	mu, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	if append {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, mode)
		if err != nil {
			return f.Error(err)
		}
		defer file.Close()

		if _, err := file.WriteString(data); err != nil {
			return f.Error(err)
		}
	} else {
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			return f.Error(err)
		}
	}

	return f.Push(lua.LTrue)
}

func (f *Fs) glob(L *lua.LState) int {
	pattern := L.CheckString(1)

	files, err := filepath.Glob(pattern)
	if err != nil {
		return f.Error(err)
	}
	result := L.CreateTable(len(files), 0)
	for _, file := range files {
		result.Append(lua.LString(file))
	}
	return f.Push(result)
}

func (f *Fs) join(L *lua.LState) int {
	elems := make([]string, L.GetTop())
	for i := 1; i <= L.GetTop(); i++ {
		elems[i-1] = L.CheckString(i)
	}
	return f.Push(lua.LString(filepath.Join(elems...)))
}

func (f *Fs) clean(L *lua.LState) int {
	path := L.CheckString(1)
	return f.Push(lua.LString(filepath.Clean(path)))
}

func (f *Fs) abspath(L *lua.LState) int {
	path := L.CheckString(1)
	ret, err := filepath.Abs(path)
	if err != nil {
		return f.Error(err)
	}
	return f.Push(lua.LString(ret))
}

func (f *Fs) isabs(L *lua.LState) int {
	path := L.CheckString(1)
	return f.Push(lua.LBool(filepath.IsAbs(path)))
}

func (f *Fs) copyFile(L *lua.LState) int {
	src := L.CheckString(1)
	dest := L.CheckString(2)

	sf, err := os.Open(src)
	if err != nil {
		return f.Error(err)
	}
	defer sf.Close()

	si, err := sf.Stat()
	if err != nil {
		return f.Error(err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return f.Error(err)
	}

	df, err := os.Create(dest)
	if err != nil {
		return f.Error(err)
	}
	defer df.Close()

	if _, err := io.Copy(df, sf); err != nil {
		return f.Error(err)
	}

	if err := os.Chmod(dest, si.Mode()); err != nil {
		return f.Error(err)
	}

	return f.Push(lua.LTrue)
}

func (f *Fs) moveFile(L *lua.LState) int {
	src := L.CheckString(1)
	dest := L.CheckString(2)

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return f.Error(err)
	}

	if err := os.Rename(src, dest); err != nil {
		return f.Error(err)
	}

	return f.Push(lua.LTrue)
}

func (f *Fs) chmod(L *lua.LState) int {
	path := L.CheckString(1)
	mode, err := oct2decimal(L.CheckInt(2))
	if err != nil {
		return f.Error(err)
	}
	if err := os.Chmod(path, os.FileMode(mode)); err != nil {
		return f.Error(err)
	}
	return f.Push(lua.LTrue)
}

func (f *Fs) fromSlash(L *lua.LState) int {
	path := L.CheckString(1)
	return f.Push(lua.LString(filepath.FromSlash(path)))
}

func (f *Fs) toSlash(L *lua.LState) int {
	path := L.CheckString(1)
	return f.Push(lua.LString(filepath.ToSlash(path)))
}

func oct2decimal(oct int) (uint64, error) {
	return strconv.ParseUint(fmt.Sprintf("%d", oct), 8, 32)
}
