package sql

import (
	"database/sql"
	"lug/util"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type (
	dbConfig struct {
		shared       bool
		maxOpenConns int
		maxIdleConns int
		dsn          string
		driver       string
	}
	txConfig struct {
		options *sql.TxOptions
		handler *lua.LFunction
		timeout time.Duration
	}
)

func getConfig(L *lua.LState) *dbConfig {
	driver := strings.TrimSpace(L.CheckString(1))
	dsn := strings.TrimSpace(L.CheckString(2))
	lopts := L.OptTable(3, L.NewTable())

	if driver == "" {
		L.ArgError(1, "driver cannot be specified")
	}
	if dsn == "" {
		L.ArgError(2, "dsn cannot be empty")
	}
	config := &dbConfig{
		maxOpenConns: 1,
		maxIdleConns: 2,
		dsn:          dsn,
		driver:       driver,
	}

	lopts.ForEach(func(k lua.LValue, v lua.LValue) {
		key := k.String()
		switch key {
		case `shared`:
			if val, ok := util.CheckBool(L, key, v, 3); ok {
				config.shared = val
			}

		case `maxOpenConns`:
			if val, ok := util.CheckInt(L, key, v, 3); ok {
				if val < 0 {
					L.ArgError(3, "maxOpenConns must be non-negative")
				}
				config.maxOpenConns = val
			}

		case `maxIdleConns`:
			if val, ok := util.CheckInt(L, key, v, 3); ok {
				if val < 0 {
					L.ArgError(3, "maxIdleConns must be non-negative")
				}
				config.maxOpenConns = val
			}

		}
	})

	return config
}

func getTxOptions(L *lua.LState) *txConfig {
	handler := L.CheckFunction(1)
	lopts := L.OptTable(2, L.NewTable())

	config := &txConfig{
		handler: handler,
		options: &sql.TxOptions{
			Isolation: sql.LevelDefault,
			ReadOnly:  false,
		},
		timeout: 100 * time.Millisecond,
	}

	lopts.ForEach(func(k lua.LValue, v lua.LValue) {
		key := k.String()
		switch key {
		case `isolation`:
			if val, ok := util.CheckInt(L, key, v, 2); ok {
				if val >= 0 && val <= 7 {
					config.options.Isolation = sql.IsolationLevel(val)
				}
			}
		case `readOnly`:
			if val, ok := util.CheckBool(L, key, v, 2); ok {
				config.options.ReadOnly = val
			}

		case `timeout`:
			if val, ok := util.CheckInt(L, key, v, 2); ok {
				config.timeout = time.Duration(val) * time.Second
			}
		}

	})
	return config
}
