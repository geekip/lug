package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime/pprof"

	"lug/libs"
	"lug/pkg"
	"lug/util"
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

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize Lua state
	L := util.VmPool.Get()
	defer func() {
		util.VmPool.Put(L)
		cancel()
	}()

	L.SetContext(ctx)

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
