package http

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"lug/util"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type (
	serveFileOpts struct {
		ignoreBase  bool
		autoIndex   bool
		index       []string
		prettyIndex bool
	}
	dirEntry struct {
		Name  string
		IsDir bool
	}

	dirLists struct {
		DirName string
		Files   []dirEntry
	}
)

const defaultMultipartMemory = 32 << 20 // 32 MB

var defaultIndexes = []string{
	"index.html",
	"index.htm",
	"default.html",
	"default.htm",
}

var mimes = map[string]string{
	".sh":   "text/x-sh",
	".yaml": "text/yaml",
	".yml":  "text/yaml",
}

func init() {
	for k, v := range mimes {
		mime.AddExtensionType(k, v)
	}
}

func serveFile(w http.ResponseWriter, r *http.Request, filePath, fileName string, opts ...*serveFileOpts) (int64, int, error) {
	fullPath := filePath
	if fileName != "" {
		fullPath = filepath.Join(fullPath, fileName)
	}

	if !isPathSafe(filePath, fullPath) {
		return 0, http.StatusForbidden, errors.New("path traversal attempt detected")
	}

	file, err := getFile(fullPath)
	if err != nil {
		statusCode, e := handleFileError(err)
		return 0, statusCode, e
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return 0, http.StatusInternalServerError, err
	}

	if info.IsDir() {
		var opt *serveFileOpts
		if len(opts) > 0 {
			opt = opts[0]
		}
		return handleDirectory(w, r, fullPath, fileName, file, opt)
	}

	writeFile(w, r, file, info)
	return info.Size(), http.StatusOK, nil
}

func writeFile(w http.ResponseWriter, r *http.Request, file http.File, info fs.FileInfo) {
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
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, filename, modTime, file)
}

func isPathSafe(root, target string) bool {
	relPath, err := filepath.Rel(root, target)
	return err == nil && !strings.HasPrefix(relPath, "..") && relPath != ".."
}

func handleFileError(err error) (int, error) {
	if os.IsNotExist(err) {
		return http.StatusNotFound, err
	}
	if os.IsPermission(err) {
		return http.StatusForbidden, err
	}
	return http.StatusInternalServerError, err
}

func handleDirectory(w http.ResponseWriter, r *http.Request, filePath, fileName string, file http.File, opt *serveFileOpts) (int64, int, error) {
	if opt != nil && opt.autoIndex {
		if file, info := findIndexFile(opt, filePath); file != nil {
			defer file.Close()
			http.ServeContent(w, r, info.Name(), info.ModTime(), file)
			return info.Size(), http.StatusOK, nil
		}
	}

	if opt != nil && opt.ignoreBase {
		return 0, http.StatusForbidden, errors.New("directory access forbidden")
	}

	return dirList(w, file, fileName, opt)
}

func findIndexFile(opt *serveFileOpts, filePath string) (http.File, fs.FileInfo) {
	for _, index := range opt.index {
		fullPath := filepath.Join(filePath, index)
		file, err := getFile(fullPath)
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

func dirList(w http.ResponseWriter, file http.File, fileName string, opt *serveFileOpts) (int64, int, error) {
	fileName = path.Clean("/" + fileName)
	if !strings.HasSuffix(fileName, "/") {
		fileName += "/"
	}

	files, err := file.Readdir(-1)
	if err != nil {
		return 0, http.StatusInternalServerError, err
	}

	sort.Slice(files, func(i, j int) bool {
		a, b := files[i], files[j]
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		return a.Name() < b.Name()
	})

	data := dirLists{
		DirName: fileName,
		Files:   make([]dirEntry, len(files)),
	}

	for i, f := range files {
		data.Files[i] = dirEntry{
			Name:  f.Name(),
			IsDir: f.IsDir(),
		}
	}

	tplStr := dirTemplatePure
	if opt != nil && opt.prettyIndex {
		tplStr = dirTemplatePretty
	}

	var buf bytes.Buffer
	var size int64
	if tpl, e := util.ParseTemplateString(tplStr, "LUG_TPL_DIRLIST"); e == nil {
		if err := tpl.Execute(&buf, data); err != nil {
			return 0, http.StatusInternalServerError, err
		}
	}

	size, err = buf.WriteTo(w)
	if err != nil {
		return 0, http.StatusInternalServerError, err
	}

	return size, http.StatusOK, nil
}

func getFile(path string) (http.File, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	fsys := http.Dir(dir)
	return fsys.Open(base)
}

func uploadFile(r *http.Request, fieldName, dst string, modes ...fs.FileMode) error {

	var mode os.FileMode = 0o750
	if len(modes) > 0 {
		mode = modes[0]
	}

	if err := r.ParseMultipartForm(defaultMultipartMemory); err != nil {
		return err
	}

	fhs, ok := r.MultipartForm.File[fieldName]
	if !ok || len(fhs) == 0 {
		f, fh, err := r.FormFile(fieldName)
		if err != nil {
			return fmt.Errorf("no files found for key '%s'", fieldName)
		}
		defer f.Close()
		fhs = []*multipart.FileHeader{fh}
	}

	for _, fh := range fhs {
		dstPath := filepath.Join(dst, fh.Filename)
		if err := saveFile(fh, dstPath, mode); err != nil {
			return err
		}
	}

	return nil
}

func saveFile(fh *multipart.FileHeader, dst string, mode fs.FileMode) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dirMode := mode | 0o100
	dir := filepath.Dir(dst)
	if err = os.MkdirAll(dir, dirMode); err != nil {
		return err
	}

	if err := os.Chmod(dir, dirMode); err != nil {
		return err
	}

	fileMode := mode &^ 0o111
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, src); err != nil {
		return err
	}

	return os.Chmod(dst, fileMode)
}

func attachment(w http.ResponseWriter, r *http.Request, filePath, fileName string) (int64, int, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return 0, http.StatusInternalServerError, err
	}
	if stat.IsDir() {
		return 0, http.StatusInternalServerError, errors.New("attachment file must be a file")
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	return serveFile(w, r, filePath, fileName)
}
