package util

import "log"

var (
	originalPrintln = log.Println
)

func CustomPrintln(v ...interface{}) {
	// 在这里添加自定义处理逻辑
	// 例如：添加时间戳、日志级别等

	// 示例：添加前缀
	prefixed := append([]interface{}{"[拦截] "}, v...)

	// 调用原始log.Println
	originalPrintln(prefixed...)
}
