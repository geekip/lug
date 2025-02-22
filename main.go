package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"

	"github.com/chzyer/readline"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
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
`, PackageName)
	}
	flag.Parse()
	nargs := flag.NArg()

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
	if optExecute == "" && !optInteractive && !optVersion && nargs == 0 {
		optInteractive = true
	}

	// Initialize Lua state
	L := lua.NewState()
	defer L.Close()

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
	L.SetGlobal("arg", arg)

	// Preload Lua modules
	for name, fn := range luaLibs {
		L.PreloadModule(name, fn)
	}

	// Show version information
	if optVersion || optInteractive {
		fmt.Println(PackageCopyRight)
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

	// Execute script file
	if nargs > 0 {
		if err := executeScript(L, scriptPath, optDumpAST, optDumpCode); err != nil {
			return err
		}
	}

	// Enter interactive mode
	if optInteractive {
		doREPL(L)
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
	var scriptPath string
	if optExecute != "" {
		hArgs = append(hArgs, optExecute)
	}

	args := flag.Args()
	nargs := flag.NArg()

	var packagePath string
	if nargs > 0 {
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

	// Get Lua package path
	packagePath = `package.path='` + workDir + `/?.lua;'..package.path`

	return scriptPath, packagePath, Largs, nil
}

func executeScript(L *lua.LState, scriptPath string, dumpAST, dumpVM bool) error {

	if dumpAST || dumpVM {
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
		if dumpVM {
			proto, err := lua.Compile(chunk, scriptPath)
			if err != nil {
				return err
			}
			fmt.Println(proto.String())
		}
	}

	// Execute script
	if err := L.DoFile(scriptPath); err != nil {
		return err
	}
	return nil
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

// doREPL implements the Read-Eval-Print Loop (REPL).
func doREPL(L *lua.LState) {
	rl, err := readline.New("> ")
	if err != nil {
		panic(err)
	}
	defer rl.Close()

	for {
		line, err := loadline(rl, L)
		if err != nil {
			if err == readline.ErrInterrupt {
				break
			}
			fmt.Println(err)
			continue
		}
		if err := L.DoString(line); err != nil {
			fmt.Println(err)
		}
	}
}

// loadline reads a single line of input and handles multiline fallback.
func loadline(rl *readline.Instance, L *lua.LState) (string, error) {
	rl.SetPrompt("> ")
	line, err := rl.Readline()
	if err != nil {
		return "", err
	}

	// Try compiling as a return statement
	if _, err := L.LoadString("return " + line); err == nil {
		return line, nil
	}

	// Handle multiline input
	return multiline(line, rl, L)
}

// multiline collects multiline input until a valid Lua chunk is formed.
func multiline(ml string, rl *readline.Instance, L *lua.LState) (string, error) {
	var sb strings.Builder
	sb.WriteString(ml)

	for {
		if _, err := L.LoadString(sb.String()); err == nil {
			return sb.String(), nil
		} else if !isIncompleteError(err) {
			return sb.String(), nil
		}
		rl.SetPrompt(">> ")
		line, err := rl.Readline()
		if err != nil {
			return "", err
		}
		sb.WriteString("\n")
		sb.WriteString(line)
	}
}

// isIncompleteError checks if an error indicates incomplete input.
func isIncompleteError(err error) bool {
	if apiErr, ok := err.(*lua.ApiError); ok {
		if parseErr, ok := apiErr.Cause.(*parse.Error); ok {
			return parseErr.Pos.Line == parse.EOF
		}
	}
	return false
}
