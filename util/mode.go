package util

import (
	"os"
	"sync/atomic"
)

const (
	DebugMode   = "debug"
	ReleaseMode = "release"
	TestMode    = "test"
)

const (
	debugCode   = iota
	releaseCode = iota
	testCode    = iota
)

var systemMode int32 = debugCode
var modeName atomic.Value

func init() {
	mode := os.Getenv("LUG_MODE")
	if len(mode) == 0 {
		mode = DebugMode
	}
	SetMode(mode)
}

func SetMode(mode string) {
	switch mode {
	case DebugMode, "":
		atomic.StoreInt32(&systemMode, debugCode)
	case ReleaseMode:
		atomic.StoreInt32(&systemMode, releaseCode)
	case TestMode:
		atomic.StoreInt32(&systemMode, testCode)
	default:
		panic("system mode unknown: " + mode + " (available mode: debug release test)")
	}
	modeName.Store(mode)
}

func GetMode() string {
	return modeName.Load().(string)
}
