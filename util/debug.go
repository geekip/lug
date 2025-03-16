package util

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
)

var (
	DefaultWriter      io.Writer = os.Stdout
	DefaultErrorWriter io.Writer = os.Stderr
	debugPrintFunc     func(format string, values ...interface{})
	debugPrefix        string = "[Lug-debug] "
	debugErrorPrefix   string = debugPrefix + "[ERROR] "
)

func IsDebug() bool {
	return atomic.LoadInt32(&systemMode) == debugCode
}

func debugPrint(writer io.Writer, format string, a ...any) {
	if !IsDebug() {
		return
	}
	if debugPrintFunc != nil {
		debugPrintFunc(format, a...)
		return
	}
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	fmt.Fprintf(writer, format, a...)
}

func DebugPrint(a ...any) {
	debugPrint(DefaultWriter, debugPrefix+"%s", a...)
}

func DebugPrintError(err error) {
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	stack := buf[:n]
	debugPrint(DefaultErrorWriter, debugErrorPrefix+"%s\n%s", err, stack)
}
