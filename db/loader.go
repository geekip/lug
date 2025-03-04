package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/geekip/lug/util"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
	lua "github.com/yuin/gopher-lua"
)

type DB struct {
	util.Module
	db      *sql.DB
	table   string
	fields  string
	where   string
	groupBy string
	having  string
	orderBy string
	limit   string
	query   string
	args    []interface{}
	err     error
}

func Loader(L *lua.LState) int {
	api := map[string]lua.LGFunction{
		"open": new,
	}
	L.Push(L.SetFuncs(L.NewTable(), api))
	return 1
}

func new(L *lua.LState) int {
	Type := L.CheckString(1)
	dsn := L.CheckString(2)

	var db *sql.DB
	var err error

	switch Type {
	case "sqlite":
		db, err = sql.Open("sqlite3", dsn)
	case "mysql":
		db, err = sql.Open("mysql", dsn)
	default:
		err = fmt.Errorf("unsupported database type: %s", Type)
	}

	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err = db.PingContext(ctx); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	mod := &DB{
		util.GetModule(L), db,
		"", "", "", "", "", "", "", "", []interface{}{}, nil,
	}

	api := util.LGFunctions{
		"table":  mod.Table,
		"field":  mod.Field,
		"where":  mod.Where,
		"group":  mod.Group,
		"having": mod.Having,
		"order":  mod.Order,
		"limit":  mod.Limit,
		"query":  mod.Query,
		"exec":   mod.Exec,
		"insert": mod.Insert,
		"update": mod.Update,
		"delete": mod.Delete,
		"find":   mod.Find,
		"select": mod.Select,
		"count":  mod.Count,
		"close":  mod.Close,
	}

	return mod.Api(api)
}
