package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"

	"lug/libs"
	pkg "lug/package"
	"lug/util"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

func Run() error {
	var (
		optExecute     string
		optLibrary     string
		optProfile     string
		optMemoryLimit int
		optInteractive bool
		optVersion     bool
		optDumpAST     bool
		optDumpCode    bool
	)

	flag.StringVar(&optExecute, "e", "", "")
	flag.StringVar(&optLibrary, "l", "", "")
	flag.IntVar(&optMemoryLimit, "m", 0, "")
	flag.BoolVar(&optDumpAST, "dt", false, "")
	flag.BoolVar(&optDumpCode, "dc", false, "")
	flag.BoolVar(&optInteractive, "i", false, "")
	flag.StringVar(&optProfile, "p", "", "")
	flag.BoolVar(&optVersion, "v", false, "")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [options] [script [args]]
Available options are:
  -e stat  execute string 'stat'
  -i       enter interactive mode after executing 'script'
  -l name  require library 'name'
  -m MB    memory limit (default: unlimited)
  -dt      dump AST trees
  -dc      dump VM codes
  -p file  write cpu profile to the file
  -v       show version information
`, pkg.Name)
	}
	flag.Parse()

	// Setup CPU profiling
	if optProfile != "" {
		f, err := os.Create(optProfile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}

	// Default to interactive mode if no options provided
	if optExecute == "" && !optInteractive && !optVersion && flag.NArg() == 0 {
		optInteractive = true
	}

	// Initialize Lua state
	L := util.VmPool.Get()
	defer util.VmPool.Put(L)

	// L := lua.NewState()
	// defer L.Close()

	// Set memory limit
	if optMemoryLimit > 0 {
		L.SetMx(optMemoryLimit)
	}

	scriptPath, packagePath, arg, err := executeArgs(L, optExecute)
	if err != nil {
		return err
	}

	// Set Lua package.path
	if err := L.DoString(packagePath); err != nil {
		return fmt.Errorf("failed to set package.path: %w", err)
	}

	// Set Lua arg table
	L.SetGlobal(`arg`, arg)

	// Preload Lua modules
	libs.Preload(L)

	// Show version information
	if optVersion || optInteractive {
		fmt.Println(pkg.CopyRight)
	}

	// Load library if specified
	if optLibrary != "" {
		if err := L.DoFile(optLibrary); err != nil {
			return err
		}
	}

	// Execute command line script
	if optExecute != "" {
		if err := L.DoString(optExecute); err != nil {
			return err
		}
	}

	if scriptPath != "" {
		if optDumpAST || optDumpCode {
			// Execute script file and Dump AST or VM Code
			if err := executeDump(L, scriptPath, optDumpAST, optDumpCode); err != nil {
				return err
			}
		} else {
			// Execute script file
			if err := L.DoFile(scriptPath); err != nil {
				return err
			}
		}
	}

	// Enter interactive mode
	if optInteractive {
		if err := doREPL(L); err != nil {
			return err
		}
	}
	return nil
}

func executeArgs(L *lua.LState, optExecute string) (string, string, *lua.LTable, error) {

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
	Largs := L.NewTable()
	for i, arg := range args {
		Largs.RawSet(lua.LNumber(i-hLen), lua.LString(arg))
	}

	// Fix path issues in Windows
	workDir = filepath.ToSlash(workDir)

	// Get Lua package path
	packagePath := "package.path='" + workDir + "/?.lua;'..package.path"
	return scriptPath, packagePath, Largs, nil
}

func executeDump(L *lua.LState, scriptPath string, dumpAST, dumpVM bool) error {

	// Read script content once
	file, err := os.Open(scriptPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Parse script content
	chunk, err := parse.Parse(file, scriptPath)
	if err != nil {
		return err
	}

	// Dump AST if requested
	if dumpAST {
		fmt.Println(parse.Dump(chunk))
	}

	// Compile and optionally dump VM code
	proto, err := lua.Compile(chunk, scriptPath)
	if err != nil {
		return err
	}

	if dumpVM {
		fmt.Println(proto.String())
	}

	lFunc := L.NewFunctionFromProto(proto)
	L.Push(lFunc)
	return L.PCall(0, lua.MultRet, nil)
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
