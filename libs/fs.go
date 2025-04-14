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

type Fs struct{}

var fileLocks sync.Map

func FsLoader(L *lua.LState) int {
	instance := &Fs{}
	api := util.SetMethods(L, util.Methods{
		"mkdir":     instance.mkdir,
		"copy":      instance.copyFile,
		"chmod":     instance.chmod,
		"move":      instance.moveFile,
		"remove":    instance.remove,
		"read":      instance.read,
		"write":     instance.write,
		"isdir":     instance.isdir,
		"dirname":   instance.dirname,
		"basename":  instance.basename,
		"exedir":    instance.exedir,
		"cwdir":     instance.cwdir,
		"symlink":   instance.symlink,
		"ext":       instance.ext,
		"exists":    instance.exists,
		"glob":      instance.glob,
		"join":      instance.join,
		"clean":     instance.clean,
		"abspath":   instance.abspath,
		"isabs":     instance.isabs,
		"fromSlash": instance.fromSlash,
		"toSlash":   instance.toSlash,
	})
	return util.Push(L, api)
}

func (f *Fs) mkdir(L *lua.LState) int {
	path, recursive := L.CheckString(1), L.OptBool(2, true)
	mode := os.FileMode(0755)

	if L.GetTop() >= 3 {
		m, err := oct2decimal(L.CheckInt(3))
		if err != nil {
			return util.NilError(L, err)
		}
		mode = os.FileMode(m)
	}

	if recursive {
		if err := os.MkdirAll(path, mode); err != nil {
			return util.NilError(L, err)
		}
	} else {
		if err := os.Mkdir(path, mode); err != nil {
			return util.NilError(L, err)
		}
	}
	return util.Push(L, lua.LTrue)
}

func (f *Fs) remove(L *lua.LState) int {
	path, recursive := L.CheckString(1), L.OptBool(2, false)

	if recursive {
		if err := os.RemoveAll(path); err != nil {
			return util.NilError(L, err)
		}
	} else {
		if err := os.Remove(path); err != nil {
			return util.NilError(L, err)
		}
	}
	return util.Push(L, lua.LTrue)
}

func (f *Fs) isdir(L *lua.LState) int {
	path := L.CheckString(1)
	stat, err := os.Stat(path)
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LBool(stat.IsDir()))
}

func (f *Fs) dirname(L *lua.LState) int {
	return f._dirname(L, L.CheckString(1))
}

func (f *Fs) basename(L *lua.LState) int {
	path := L.CheckString(1)
	return util.Push(L, lua.LString(filepath.Base(path)))
}

func (f *Fs) exedir(L *lua.LState) int {
	path, err := os.Executable()
	if err != nil {
		return util.NilError(L, err)
	}
	return f._dirname(L, path)
}

func (f *Fs) _dirname(L *lua.LState, path string) int {
	return util.Push(L, lua.LString(filepath.Dir(path)))
}

func (f *Fs) cwdir(L *lua.LState) int {
	path, err := os.Getwd()
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(path))
}

func (f *Fs) symlink(L *lua.LState) int {
	target, link := L.CheckString(1), L.CheckString(2)
	if err := os.Symlink(target, link); err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LTrue)
}

func (f *Fs) ext(L *lua.LState) int {
	return util.Push(L, lua.LString(filepath.Ext(L.CheckString(1))))
}

func (f *Fs) exists(L *lua.LState) int {
	_, err := os.Stat(L.CheckString(1))
	return util.Push(L, lua.LBool(!os.IsNotExist(err)))
}

func (f *Fs) read(L *lua.LState) int {
	path := L.CheckString(1)
	content, err := os.ReadFile(path)
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(content))
}

func (f *Fs) write(L *lua.LState) int {
	path, data, append := L.CheckString(1), L.CheckString(2), L.OptBool(3, false)
	mode := os.FileMode(0644)

	if L.GetTop() >= 4 {
		if m, err := oct2decimal(L.CheckInt(4)); err != nil {
			return util.NilError(L, err)
		} else {
			mode = os.FileMode(m)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), mode|0755); err != nil {
		return util.NilError(L, err)
	}

	mu, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	if append {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, mode)
		if err != nil {
			return util.NilError(L, err)
		}
		defer file.Close()

		if _, err := file.WriteString(data); err != nil {
			return util.NilError(L, err)
		}
	} else {
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			return util.NilError(L, err)
		}
	}

	return util.Push(L, lua.LTrue)
}

func (f *Fs) glob(L *lua.LState) int {
	pattern := L.CheckString(1)

	files, err := filepath.Glob(pattern)
	if err != nil {
		return util.NilError(L, err)
	}
	result := L.CreateTable(len(files), 0)
	for _, file := range files {
		result.Append(lua.LString(file))
	}
	return util.Push(L, result)
}

func (f *Fs) join(L *lua.LState) int {
	elems := make([]string, L.GetTop())
	for i := 1; i <= L.GetTop(); i++ {
		elems[i-1] = L.CheckString(i)
	}
	return util.Push(L, lua.LString(filepath.Join(elems...)))
}

func (f *Fs) clean(L *lua.LState) int {
	path := L.CheckString(1)
	return util.Push(L, lua.LString(filepath.Clean(path)))
}

func (f *Fs) abspath(L *lua.LState) int {
	path := L.CheckString(1)
	ret, err := filepath.Abs(path)
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LString(ret))
}

func (f *Fs) isabs(L *lua.LState) int {
	path := L.CheckString(1)
	return util.Push(L, lua.LBool(filepath.IsAbs(path)))
}

func (f *Fs) copyFile(L *lua.LState) int {
	src, dest := L.CheckString(1), L.CheckString(2)

	sf, err := os.Open(src)
	if err != nil {
		return util.NilError(L, err)
	}
	defer sf.Close()

	si, err := sf.Stat()
	if err != nil {
		return util.NilError(L, err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return util.NilError(L, err)
	}

	df, err := os.Create(dest)
	if err != nil {
		return util.NilError(L, err)
	}
	defer df.Close()

	if _, err := io.Copy(df, sf); err != nil {
		return util.NilError(L, err)
	}

	if err := os.Chmod(dest, si.Mode()); err != nil {
		return util.NilError(L, err)
	}

	return util.Push(L, lua.LTrue)
}

func (f *Fs) moveFile(L *lua.LState) int {
	src, dest := L.CheckString(1), L.CheckString(2)

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return util.NilError(L, err)
	}

	if err := os.Rename(src, dest); err != nil {
		return util.NilError(L, err)
	}

	return util.Push(L, lua.LTrue)
}

func (f *Fs) chmod(L *lua.LState) int {
	path := L.CheckString(1)
	mode, err := oct2decimal(L.CheckInt(2))
	if err != nil {
		return util.NilError(L, err)
	}
	if err := os.Chmod(path, os.FileMode(mode)); err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LTrue)
}

func (f *Fs) fromSlash(L *lua.LState) int {
	path := L.CheckString(1)
	return util.Push(L, lua.LString(filepath.FromSlash(path)))
}

func (f *Fs) toSlash(L *lua.LState) int {
	path := L.CheckString(1)
	return util.Push(L, lua.LString(filepath.ToSlash(path)))
}

func oct2decimal(oct int) (uint64, error) {
	return strconv.ParseUint(fmt.Sprintf("%d", oct), 8, 32)
}
