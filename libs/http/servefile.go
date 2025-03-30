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

func serveFile(L *lua.LState, root string, lopts *lua.LTable, res *Response) (int, error) {

	var dirName string
	if res.Request.PathValue("prefix") != "" {
		dirName = res.Request.PathValue("path")
	}
	fullPath := path.Join(root, dirName)

	if !isPathSafe(root, fullPath) {
		return http.StatusForbidden, errors.New("path traversal attempt detected")
	}

	file, err := getfile(fullPath)
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
		return handleDirectory(fullPath, dirName, file, opts, res)
	}

	res.Size = int(info.Size())
	writeFile(file, info, res)
	return 200, nil
}

func writeFile(file http.File, info fs.FileInfo, res *Response) {
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
	res.ResponseWriter.Header().Set("Content-Type", contentType)
	http.ServeContent(res.ResponseWriter, res.Request, filename, modTime, file)
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

func handleDirectory(root, dirName string, file http.File, opts *serveFileOpts, res *Response) (int, error) {

	if opts.ignorebase {
		return http.StatusForbidden, errors.New("directory access forbidden")
	}

	if opts.autoindex {
		if file, info := findIndexFile(opts, root); file != nil {
			defer file.Close()
			res.Size = int(info.Size())
			http.ServeContent(res.ResponseWriter, res.Request, info.Name(), info.ModTime(), file)
			return 0, nil
		}
	}

	if statusCode, err := dirList(file, dirName, opts, res); err != nil {
		return statusCode, err
	}
	return 0, nil
}

func getfile(path string) (http.File, error) {
	fsys := http.Dir(filepath.Dir(path))
	return fsys.Open(filepath.Base(path))
}

func findIndexFile(opts *serveFileOpts, dirPath string) (http.File, fs.FileInfo) {
	for _, index := range opts.index {
		file, err := getfile(filepath.Join(dirPath, index))
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

func dirList(file http.File, dirName string, opts *serveFileOpts, res *Response) (int, error) {
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
		size, err = buf.WriteTo(res.ResponseWriter)
	}

	if err != nil {
		return http.StatusInternalServerError, err
	}

	res.Size = int(size)

	return 0, nil
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
			if val, ok := v.(lua.LBool); ok {
				opts.ignorebase = bool(val)
			} else {
				L.RaiseError("ignorebase must be a boolean")
			}

		case "autoindex":
			if val, ok := v.(lua.LBool); ok {
				opts.autoindex = bool(val)
			} else {
				L.RaiseError("autoindex must be a boolean")
			}

		case "index":
			if tbl, ok := v.(*lua.LTable); ok {
				opts.index = parseStringTable(L, tbl)
			} else {
				L.RaiseError("index must be a array table")
			}

		case "prettyindex":
			if val, ok := v.(lua.LBool); ok {
				opts.prettyindex = bool(val)
			} else {
				L.RaiseError("prettyindex must be a boolean")
			}

		}
	})
	return opts
}

func parseStringTable(L *lua.LState, tbl *lua.LTable) []string {
	var result []string
	tbl.ForEach(func(_, lv lua.LValue) {
		if str, ok := lv.(lua.LString); ok {
			result = append(result, str.String())
		} else {
			L.RaiseError("index table contains non-string value")
		}
	})
	return result
}
