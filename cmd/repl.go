package cmd

import (
	"fmt"
	"strings"

	"github.com/chzyer/readline"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

// doREPL implements the Read-Eval-Print Loop (REPL).
func doREPL(L *lua.LState) error {
	rl, err := readline.New("> ")
	if err != nil {
		return err
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
	return nil
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
