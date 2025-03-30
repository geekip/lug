package sql

import (
	"database/sql"
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
		options  *sql.TxOptions
		callback *lua.LFunction
		timeout  time.Duration
	}
)

func getConfig(L *lua.LState) *dbConfig {
	driver := strings.TrimSpace(L.CheckString(1))
	dsn := strings.TrimSpace(L.CheckString(2))

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
	if L.GetTop() > 2 {
		L.CheckTable(3).ForEach(func(k lua.LValue, v lua.LValue) {
			if k.String() == `shared` {
				if val, ok := v.(lua.LBool); ok {
					config.shared = bool(val)
				} else {
					L.ArgError(3, "shared must be bool")
				}
			}
			if k.String() == `maxOpenConns` {
				if val, ok := v.(lua.LNumber); ok {
					num := int(val)
					if num < 0 {
						L.ArgError(3, "maxOpenConns must be non-negative")
					}
					config.maxOpenConns = num
				} else {
					L.ArgError(3, "maxOpenConns must be number")
				}
			}
			if k.String() == `maxIdleConns` {
				if val, ok := v.(lua.LNumber); ok {
					num := int(val)
					if num < 0 {
						L.ArgError(3, "maxIdleConns must be non-negative")
					}
					config.maxIdleConns = num
				} else {
					L.ArgError(3, "maxIdleConns must be number")
				}
			}
		})
	}
	return config
}

func getTxOptions(L *lua.LState) *txConfig {
	callback := L.CheckFunction(1)
	lopts := L.OptTable(2, L.NewTable())

	config := &txConfig{
		callback: callback,
		options: &sql.TxOptions{
			Isolation: sql.LevelDefault,
			ReadOnly:  false,
		},
		timeout: 100 * time.Millisecond,
	}

	lopts.ForEach(func(k lua.LValue, v lua.LValue) {
		if k.String() == `isolation` {
			if val, ok := v.(lua.LNumber); ok {
				num := int(val)
				if num >= 0 && num <= 7 {
					config.options.Isolation = sql.IsolationLevel(num)
				}
			} else {
				L.ArgError(1, "isolation must be number")
			}
		}

		if k.String() == `readOnly` {
			if val, ok := v.(lua.LBool); ok {
				config.options.ReadOnly = bool(val)
			} else {
				L.ArgError(1, "readOnly must be bool")
			}
		}

		if k.String() == `timeout` {
			if val, ok := v.(lua.LNumber); ok {
				config.timeout = time.Duration(val) * time.Second
			} else {
				L.ArgError(1, "timeout must be number(S)")
			}
		}
	})
	return config
}
