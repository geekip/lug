package cmd

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

func executeArgs(optExecute string) (string, string, *lua.LTable, error) {

	// Get executable path
	exePath, err := getExePath()
	if err != nil {
		return "", "", nil, err
	}

	// Determine working directory
	workDir := filepath.Dir(exePath)
	if wd, err := os.Getwd(); err == nil {
		workDir = wd
	}

	hArgs := []string{exePath}

	// Determine script source
	if optExecute != "" {
		hArgs = append(hArgs, optExecute)
	}

	var scriptPath string
	args := flag.Args()
	if flag.NArg() > 0 {
		scriptPath = filepath.Clean(args[0])
		if !filepath.IsAbs(scriptPath) {
			scriptPath = filepath.Join(workDir, scriptPath)
		}
		workDir = filepath.Dir(scriptPath)
		hArgs = append(hArgs, scriptPath)
		args = args[1:]
	}

	// merge args
	args = append(hArgs, args...)
	hLen := len(hArgs) - 1

	// Get Lua arg table
	Largs := &lua.LTable{}
	for i, arg := range args {
		Largs.RawSet(lua.LNumber(i-hLen), lua.LString(arg))
	}

	// Fix path issues in Windows
	workDir = filepath.ToSlash(workDir)

	// Get Lua package path
	packagePath := "package.path='" + workDir + "/?.lua;'..package.path"
	return scriptPath, packagePath, Largs, nil
}

// getExePath retrieves the path to the executable.
func getExePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	if strings.Contains(exePath, "go-build") {
		_, filename, _, ok := runtime.Caller(0)
		if !ok {
			return "", err
		}
		exePath = filename
	}
	if realPath, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = realPath
	}

	return filepath.Clean(exePath), nil
}
