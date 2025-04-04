package http

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"lug/util"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

type serveFileOpts struct {
	ignorebase  bool
	autoindex   bool
	index       []string
	prettyindex bool
}

type dirEntry struct {
	Name  string
	IsDir bool
}

type dirListing struct {
	DirName string
	Files   []dirEntry
}

var defaultIndexes = []string{
	"index.html",
	"index.htm",
	"default.html",
	"default.htm",
}

func init() {
	for k, v := range mimes {
		mime.AddExtensionType(k, v)
	}
}

func serveFile(L *lua.LState, root string, lopts *lua.LTable, ctx *Context) (int, error) {

	dirName := ctx.r.PathValue("path")
	fullPath := path.Join(root, dirName)

	if !isPathSafe(root, fullPath) {
		return http.StatusForbidden, errors.New("path traversal attempt detected")
	}

	file, err := getFile(fullPath)
	if err != nil {
		return handleFileError(err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return http.StatusInternalServerError, err
	}

	if info.IsDir() {
		opts := getServeFileOpts(L, lopts)
		return handleDirectory(fullPath, dirName, file, opts, ctx)
	}

	ctx.Size = int(info.Size())
	writeFile(file, info, ctx)
	return 200, nil
}

func writeFile(file http.File, info fs.FileInfo, ctx *Context) {
	filename, modTime := info.Name(), info.ModTime()
	contentType := mime.TypeByExtension(filepath.Ext(filename))
	if contentType == "" {
		buf := make([]byte, 512)
		n, err := file.Read(buf)
		if (err == nil || err == io.EOF) && n > 0 {
			contentType = http.DetectContentType(buf[:n])
			if seeker, ok := file.(io.Seeker); ok {
				_, _ = seeker.Seek(0, io.SeekStart)
			}
		} else {
			contentType = "application/octet-stream"
		}
	}
	ctx.w.Header().Set("Content-Type", contentType)
	http.ServeContent(ctx.w, ctx.r, filename, modTime, file)
}

func isPathSafe(root, target string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	relPath, err := filepath.Rel(absRoot, absTarget)
	return err == nil && !strings.HasPrefix(relPath, "..")
}

func handleFileError(err error) (int, error) {
	var statusCode int
	switch {
	case os.IsNotExist(err):
		statusCode = http.StatusNotFound
	case os.IsPermission(err):
		statusCode = http.StatusForbidden
	default:
		statusCode = http.StatusInternalServerError
	}
	return statusCode, err
}

func handleDirectory(root, dirName string, file http.File, opts *serveFileOpts, ctx *Context) (int, error) {

	if opts.ignorebase {
		return http.StatusForbidden, errors.New("directory access forbidden")
	}

	if opts.autoindex {
		if file, info := findIndexFile(opts, root); file != nil {
			defer file.Close()
			ctx.Size = int(info.Size())
			http.ServeContent(ctx.w, ctx.r, info.Name(), info.ModTime(), file)
			return 0, nil
		}
	}
	return dirList(file, dirName, opts, ctx)
}

func findIndexFile(opts *serveFileOpts, dirPath string) (http.File, fs.FileInfo) {
	for _, index := range opts.index {
		file, err := getFile(filepath.Join(dirPath, index))
		if err != nil {
			continue
		}
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			file.Close()
			continue
		}
		return file, info
	}
	return nil, nil
}

func dirList(file http.File, dirName string, opts *serveFileOpts, ctx *Context) (int, error) {
	dirName = path.Clean("/" + dirName)
	if !strings.HasSuffix(dirName, "/") {
		dirName += "/"
	}

	files, err := file.Readdir(-1)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	sort.Slice(files, func(i, j int) bool {
		a, b := files[i], files[j]
		aIsDir, bIsDir := a.IsDir(), b.IsDir()
		if aIsDir && !bIsDir {
			return true
		}
		if !aIsDir && bIsDir {
			return false
		}
		return a.Name() < b.Name()
	})

	data := dirListing{
		DirName: dirName,
		Files:   make([]dirEntry, 0, len(files)),
	}

	for _, f := range files {
		name, isDir := f.Name(), f.IsDir()
		data.Files = append(data.Files, dirEntry{
			Name:  name,
			IsDir: isDir,
		})
	}

	tplStr := dirTemplatePure
	if opts.prettyindex {
		tplStr = dirTemplatePretty
	}

	var buf bytes.Buffer
	var size int64
	if tpl, e := util.ParseTemplateString(tplStr, "LUG_TPL_DIRLIST"); e == nil {
		err = tpl.Execute(&buf, data)
	}

	if err == nil {
		size, err = buf.WriteTo(ctx.w)
	}

	if err != nil {
		return http.StatusInternalServerError, err
	}

	ctx.Size = int(size)

	return 0, nil
}

func getFile(path string) (http.File, error) {
	fsys := http.Dir(filepath.Dir(path))
	return fsys.Open(filepath.Base(path))
}

func getServeFileOpts(L *lua.LState, lopts *lua.LTable) *serveFileOpts {
	opts := &serveFileOpts{
		ignorebase:  false,
		autoindex:   true,
		index:       defaultIndexes,
		prettyindex: true,
	}

	lopts.ForEach(func(k, v lua.LValue) {
		key := k.String()
		switch key {

		case "ignorebase":
			if val, ok := util.ArgLBool(L, key, v); ok {
				opts.ignorebase = val
			}

		case "autoindex":
			if val, ok := util.ArgLBool(L, key, v); ok {
				opts.autoindex = val
			}

		case "index":
			if val, ok := util.ArgLTable(L, key, v); ok {
				opts.index = val
			}

		case "prettyindex":
			if val, ok := util.ArgLBool(L, key, v); ok {
				opts.prettyindex = val
			}

		}
	})
	return opts
}
