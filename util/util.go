package util

import (
	"fmt"
	"math"
)

func CheckStatusCode(code int) bool {
	return code >= 100 && code < 600
}

func FormatBytes(size int64) string {
	if size == 0 {
		return "0B"
	}

	sizes := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	base := 1024.0

	exp := math.Floor(math.Log(float64(size)) / math.Log(base))
	if exp > 6 {
		exp = 6
	}

	val := float64(size) / math.Pow(base, exp)
	unit := sizes[int(exp)]

	if val == math.Floor(val) {
		return fmt.Sprintf("%.0f%s", val, unit)
	} else if val < 10 {
		return fmt.Sprintf("%.2f%s", val, unit)
	} else {
		return fmt.Sprintf("%.1f%s", val, unit)
	}
}
