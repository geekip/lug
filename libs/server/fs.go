package server

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
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type (
	FileServer struct {
		config FileConfig
		w      http.ResponseWriter
		r      *http.Request
	}
	FileConfig struct {
		ignoreBase  bool
		autoIndex   bool
		index       []string
		prettyIndex bool
	}
	FileInfo struct {
		Size    int
		ModTime time.Time
		IsDir   bool
		Path    string
		Name    string
		List    []FileInfo
	}
)

const defaultMultipartMemory = 32 << 20 // 32 MB

var (
	defaultFileConfig = FileConfig{
		ignoreBase:  false,
		autoIndex:   true,
		prettyIndex: true,
		index:       defaultIndexes,
	}
	defaultIndexes = []string{
		"index.html",
		"index.htm",
		"default.html",
		"default.htm",
	}
	mimes = map[string]string{
		".sh":   "text/x-sh",
		".yaml": "text/yaml",
		".yml":  "text/yaml",
	}
	quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")
)

func init() {
	for k, v := range mimes {
		mime.AddExtensionType(k, v)
	}
}

func newFileInfo(fileInfo fs.FileInfo, filePath, fileName string) *FileInfo {
	return &FileInfo{
		Size:    int(fileInfo.Size()),
		ModTime: fileInfo.ModTime(),
		IsDir:   fileInfo.IsDir(),
		Path:    filepath.Join(filePath, fileName),
		Name:    fileName,
	}
}

func NewFileServer(w http.ResponseWriter, r *http.Request, cfg *FileConfig) *FileServer {
	config := defaultFileConfig
	if cfg != nil {
		config = *cfg
	}
	return &FileServer{w: w, r: r, config: config}
}

func (fs *FileServer) ServeFile(filePath string, fileNames ...string) (*FileInfo, HttpStatus) {

	fullPath := filepath.Clean(filePath)
	var fileName string

	if len(fileNames) > 0 {
		fileName = filepath.Clean(filepath.Join(fileNames...))
		fullPath = filepath.Join(fullPath, fileName)
	}

	// Ensure the final path is within the allowed base path
	if !isPathSafe(filePath, fullPath) {
		return nil, HttpStatus{
			Code:  http.StatusForbidden,
			Error: errors.New("path traversal attempt detected"),
		}
	}

	file, err := httpOpen(fullPath)
	if err != nil {
		statusCode, e := handleFileError(err)
		return nil, HttpStatus{Code: statusCode, Error: e}
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, HttpStatus{
			Code:  http.StatusInternalServerError,
			Error: err,
		}
	}

	fileinfo := newFileInfo(info, filePath, fileName)
	if info.IsDir() {
		return fs.handleDirectory(file, fileinfo)
	}

	return fs.serveContent(file, fileinfo)
}

func (fs *FileServer) serveContent(file http.File, info *FileInfo) (*FileInfo, HttpStatus) {

	filename, modTime := info.Name, info.ModTime
	contentType := mime.TypeByExtension(filepath.Ext(filename))

	if contentType == "" {
		// Read a small chunk to detect content type if extension is unknown
		buf := make([]byte, 512)
		n, err := io.ReadAtLeast(file, buf, 0)
		if (err == nil || errors.Is(err, io.EOF)) && n > 0 {
			contentType = http.DetectContentType(buf[:n])
			if seeker, ok := file.(io.Seeker); ok {
				_, _ = seeker.Seek(0, io.SeekStart)
			}
		} else {
			contentType = "application/octet-stream"
		}
	}

	fs.w.Header().Set("Content-Type", contentType)
	http.ServeContent(fs.w, fs.r, filename, modTime, file)
	length := int(info.Size)

	return info, HttpStatus{Length: length, Code: http.StatusOK, Error: nil}
}

func (fs *FileServer) handleDirectory(file http.File, info *FileInfo) (*FileInfo, HttpStatus) {

	if fs.config.autoIndex {
		indexFile, indexInfo := findIndexFile(fs.config.index, info.Path)
		if indexFile != nil && indexInfo != nil {
			defer indexFile.Close()

			fileInfo := newFileInfo(indexInfo, info.Path, indexInfo.Name())
			return fs.serveContent(indexFile, fileInfo)
		}
	}

	if fs.config.ignoreBase {
		return nil, HttpStatus{
			Code:  http.StatusForbidden,
			Error: errors.New("directory access forbidden"),
		}
	}

	return fs.dirList(file, info)
}

func (fs *FileServer) dirList(file http.File, info *FileInfo) (*FileInfo, HttpStatus) {

	if !strings.HasPrefix(info.Name, "/") {
		info.Name = "/" + info.Name
	}

	// Read directory entries
	infos, err := file.Readdir(-1)
	if err != nil {
		return nil, HttpStatus{
			Code:  http.StatusInternalServerError,
			Error: err,
		}
	}

	// Sort files: directories first, then by name
	sort.SliceStable(infos, func(i, j int) bool {
		a, b := infos[i], infos[j]
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		return a.Name() < b.Name()
	})

	info.List = make([]FileInfo, len(infos))
	for i, f := range infos {
		info.List[i] = *newFileInfo(f, info.Path, f.Name())
	}

	tplStr := dirTemplatePure
	if fs.config.prettyIndex {
		tplStr = dirTemplatePretty
	}

	var buf bytes.Buffer
	// Execute the template into the buffer
	tpl, err := util.ParseTemplateString(tplStr, "LUG_TPL_DIRLIST")
	if err != nil {
		return nil, HttpStatus{
			Code:  http.StatusInternalServerError,
			Error: err,
		}
	}

	if err := tpl.Execute(&buf, info); err != nil {
		return nil, HttpStatus{
			Code:  http.StatusInternalServerError,
			Error: err,
		}
	}

	size, err := buf.WriteTo(fs.w)
	if err != nil {
		return nil, HttpStatus{
			Code:  http.StatusInternalServerError,
			Error: err,
		}
	}

	return info, HttpStatus{
		Length: int(size),
		Code:   http.StatusOK,
		Error:  nil,
	}
}

func (fs *FileServer) attachment(filePath, fileName string) (*FileInfo, HttpStatus) {

	// Check if the target is a file
	stat, err := os.Stat(filePath)
	if err != nil {
		statusCode, e := handleFileError(err)
		return nil, HttpStatus{Code: statusCode, Error: e}
	}

	if stat.IsDir() {
		return nil, HttpStatus{
			Code:  http.StatusBadRequest,
			Error: errors.New("attachment file must be a file"),
		}
	}

	// Set the Content-Disposition header
	headerKey, commonVal := "Content-Disposition", "attachment; filename"
	if util.IsASCII(fileName) {
		fs.w.Header().Set(headerKey, commonVal+`="`+quoteEscaper.Replace(fileName)+`"`)
	} else {
		fs.w.Header().Set(headerKey, commonVal+`*=UTF-8''`+url.QueryEscape(fileName))
	}

	return fs.ServeFile(filePath)
}

func httpOpen(path string) (http.File, error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	return http.Dir(dir).Open(base)
}

func findIndexFile(indexFiles []string, filePath string) (http.File, fs.FileInfo) {
	for _, fileName := range indexFiles {
		fullPath := filepath.Join(filePath, fileName)
		file, err := httpOpen(fullPath)
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

func handleFileError(err error) (int, error) {
	if os.IsNotExist(err) {
		return http.StatusNotFound, err
	}
	if os.IsPermission(err) {
		return http.StatusForbidden, err
	}
	return http.StatusInternalServerError, err
}

func isPathSafe(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	if !strings.HasPrefix(absTarget, absRoot) {
		return false
	}

	suffix := absTarget[len(absRoot):]
	if len(suffix) > 0 && suffix[0] != os.PathSeparator {
		return false
	}

	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (len(rel) > 0 && !strings.HasPrefix(rel, "..") && rel != "..")
}

func uploadFile(r *http.Request, fieldName, dst string, modes ...fs.FileMode) error {

	// Standard file permissions
	var mode os.FileMode = 0o640
	if len(modes) > 0 {
		mode = modes[0]
	}

	// Parse the multipart form
	if err := r.ParseMultipartForm(defaultMultipartMemory); err != nil {
		return err
	}

	// Get file headers from the form
	fhs, ok := r.MultipartForm.File[fieldName]
	if !ok || len(fhs) == 0 {
		f, fh, err := r.FormFile(fieldName)
		if err != nil {
			return fmt.Errorf("no files found for key '%s'", fieldName)
		}
		defer f.Close()
		fhs = []*multipart.FileHeader{fh}
	}

	// Process each uploaded file
	for _, fh := range fhs {
		filename := filepath.Clean(fh.Filename)
		if strings.HasPrefix(filename, "..") {
			return fmt.Errorf("invalid filename '%s' detected", filename)
		}
		dstPath := filepath.Join(dst, filename)

		// Ensure the destination path is within the base directory 'dst'
		if !isPathSafe(dst, dstPath) {
			return fmt.Errorf("potential path traversal detected for file '%s'", fh.Filename)
		}

		if err := saveFile(fh, dstPath, mode); err != nil {
			return fmt.Errorf("failed to save file '%s': %w", fh.Filename, err)
		}
	}

	return nil
}

func saveFile(fh *multipart.FileHeader, dst string, mode fs.FileMode) error {
	// Open the uploaded file
	src, err := fh.Open()
	if err != nil {
		return fmt.Errorf("failed to open uploaded file: %w", err)
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

	// Create the destination file with the specified mode
	fileMode := mode &^ 0o111
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return err
	}
	defer out.Close()

	// Copy the uploaded file content to the destination file
	if _, err := io.Copy(out, src); err != nil {
		return err
	}

	// Ensure the file permissions are correctly set after writing
	return os.Chmod(dst, fileMode)
}
