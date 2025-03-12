package cmd

import (
	"fmt"
	"os"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

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
