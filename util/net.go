package util

func CheckStatusCode(code int) bool {
	return code >= 100 && code < 600
}
