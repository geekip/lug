package db

import (
	"context"
	"database/sql"
	"fmt"
	"lug/util"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type DB struct {
	*util.Module
	db      *SQL
	tx      *sql.Tx
	table   string
	fields  string
	where   string
	groupBy string
	having  string
	orderBy string
	limit   int
	args    []interface{}
}

func (d *DB) resetConditional() {
	d.table = ""
	d.fields = ""
	d.where = ""
	d.groupBy = ""
	d.having = ""
	d.orderBy = ""
	d.limit = 0
	d.args = nil
}

func Loader(L *lua.LState) int {
	mod := util.NewModule(L, util.Methods{
		"open": openDatabase,
	})
	return mod.Self()
}

func extendMethods(mod *DB) util.Methods {
	return util.Methods{
		"table":  mod.Table,
		"fields": mod.Fields,
		"where":  mod.Where,
		"group":  mod.Group,
		"having": mod.Having,
		"order":  mod.Order,
		"limit":  mod.Limit,
		"query":  mod.Query,
		"rows":   mod.Rows,
		"row":    mod.Row,
		"exec":   mod.Exec,
		"insert": mod.Insert,
		"update": mod.Update,
		"delete": mod.Delete,
		"count":  mod.Count,
	}
}

func openDatabase(L *lua.LState) int {
	config := getConfig(L)
	db, err := NewSQL(*config)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
		// L.RaiseError(err.Error())
		// L.ArgError(1, err.Error())
		return 0
	}

	mod := &DB{
		Module: util.NewModule(L),
		db:     db,
	}

	mod.SetMethods(extendMethods(mod), util.Methods{
		"transaction": mod.Transaction,
		"close":       mod.Close,
	})

	return mod.Self()
}

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
			if k.String() == `sharedMode` {
				if val, ok := v.(lua.LBool); ok {
					config.sharedMode = bool(val)
				} else {
					L.ArgError(3, "shared must be bool")
				}
			}
			if k.String() == `maxOpenConns` {
				if val, ok := v.(lua.LNumber); ok {
					config.maxOpenConns = int(val)
				} else {
					L.ArgError(3, "maxOpenConns must be number")
				}
			}
			if k.String() == `maxIdleConns` {
				if val, ok := v.(lua.LNumber); ok {
					config.maxIdleConns = int(val)
				} else {
					L.ArgError(3, "maxIdleConns must be number")
				}
			}
		})
	}
	return config
}

func getTxConfig(L *lua.LState) (*lua.LFunction, *sql.TxOptions, time.Duration) {
	callback := L.CheckFunction(1)
	lopts := L.OptTable(2, L.NewTable())

	opts := &sql.TxOptions{
		Isolation: sql.LevelDefault,
		ReadOnly:  false,
	}
	timeout := 100 * time.Millisecond

	lopts.ForEach(func(k lua.LValue, v lua.LValue) {
		if k.String() == `isolation` {
			if val, ok := v.(lua.LNumber); ok {
				num := int(val)
				if num >= 0 && num <= 7 {
					opts.Isolation = sql.IsolationLevel(num)
				}
			} else {
				L.ArgError(1, "isolation must be number")
			}
		}

		if k.String() == `readOnly` {
			if val, ok := v.(lua.LBool); ok {
				opts.ReadOnly = bool(val)
			} else {
				L.ArgError(1, "readOnly must be bool")
			}
		}

		if k.String() == `timeout` {
			if val, ok := v.(lua.LNumber); ok {
				timeout = time.Duration(val) * time.Second
			} else {
				L.ArgError(1, "timeout must be number(S)")
			}
		}
	})
	return callback, opts, timeout
}

func (d *DB) Transaction(L *lua.LState) int {
	callback, opts, timeout := getTxConfig(L)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tx, err := d.db.sql.BeginTx(ctx, opts)
	if err != nil {
		return d.NilError(err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		} else if err != nil {
			tx.Rollback()
		} else {
			if err = tx.Commit(); err != nil {
				tx.Rollback()
			}
		}
	}()

	mod := &DB{
		Module: util.NewModule(L),
		db:     d.db,
		tx:     tx,
	}
	mod.SetMethods(extendMethods(mod), util.Methods{
		"rollback": mod.Rollback,
		"commit":   mod.Commit,
	})

	if err := mod.CallLua(callback, mod.Method); err != nil {
		return mod.NilError(err)
	}

	ret := L.Get(-1)
	L.Pop(1)
	return mod.Push(ret, lua.LNil)
}

func (d *DB) Rollback(L *lua.LState) int {
	if err := d.tx.Rollback(); err != nil {
		return d.Error(err)
	}
	return 0
}

func (d *DB) Commit(L *lua.LState) int {
	if err := d.tx.Commit(); err != nil {
		return d.Error(err)
	}
	return 0
}

func (d *DB) Close(L *lua.LState) int {
	if err := d.db.close(); err != nil {
		return d.Error(err)
	}
	return 0
}

func (d *DB) Table(L *lua.LState) int {
	d.table = L.CheckString(1)
	return d.Self()
}

func (d *DB) Fields(L *lua.LState) int {
	top := L.GetTop()
	fields := make([]string, 0, top)
	for i := 1; i <= top; i++ {
		// fields[i-1] = L.CheckString(i)
		fields = append(fields, L.CheckString(i))
	}
	d.fields = strings.Join(fields, ",")
	return d.Self()
}

func (d *DB) Where(L *lua.LState) int {
	query, args := d.getNativeQuery()
	d.where = query
	d.args = append(d.args, args...)
	return d.Self()
}

func (d *DB) Group(L *lua.LState) int {
	d.groupBy = L.CheckString(1)
	return d.Self()
}

func (d *DB) Having(L *lua.LState) int {
	d.having = L.CheckString(1)
	return d.Self()
}

func (d *DB) Order(L *lua.LState) int {
	d.orderBy = L.CheckString(1)
	return d.Self()
}

func (d *DB) Limit(L *lua.LState) int {
	d.limit = L.CheckInt(1)
	return d.Self()
}

func (d *DB) Query(L *lua.LState) int {
	query, args := d.getNativeQuery()
	return d.query(query, args, true)
}

func (d *DB) Rows(L *lua.LState) int {
	query, args := d.getConditionalQuery()
	return d.query(query, args, true)
}

func (d *DB) Row(L *lua.LState) int {
	query, args := d.getConditionalQuery()
	return d.query(query, args, false)
}

func (d *DB) Exec(L *lua.LState) int {
	query, args := d.getNativeQuery()
	return d.exec(query, args)
}

func (d *DB) Insert(L *lua.LState) int {
	if err := d.checkTable("insert"); err != nil {
		return d.RaiseError(err)
	}
	data := L.CheckTable(1)
	dlen := data.Len()

	keys := make([]string, 0, dlen)
	placeholders := make([]string, 0, dlen)
	values := make([]interface{}, 0, dlen)

	data.ForEach(func(lk, lv lua.LValue) {
		keys = append(keys, lk.String())
		placeholders = append(placeholders, "?")
		values = append(values, lv)
	})

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		d.table,
		strings.Join(keys, ", "),
		strings.Join(placeholders, ", "),
	)
	return d.exec(query, values)
}

func (d *DB) Update(L *lua.LState) int {

	if err := d.checkTable("update"); err != nil {
		return d.NilError(err)
	}
	if err := d.checkWhere("update"); err != nil {
		return d.NilError(err)
	}

	data := L.CheckTable(1)
	dlen := data.Len()

	sets := make([]string, 0, dlen)
	values := make([]interface{}, 0, dlen)

	data.ForEach(func(lk, lv lua.LValue) {
		sets = append(sets, fmt.Sprintf("%s = ?", lk.String()))
		values = append(values, lv)
	})
	values = append(values, d.args...)

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s",
		d.table,
		strings.Join(sets, ", "),
		d.where,
	)
	return d.exec(query, values)
}

func (d *DB) Delete(L *lua.LState) int {
	if err := d.checkTable("delete"); err != nil {
		return d.NilError(err)
	}
	if err := d.checkWhere("delete"); err != nil {
		return d.NilError(err)
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", d.table, d.where)
	return d.exec(query, d.args)
}

func (d *DB) Count(L *lua.LState) int {
	query, args := d.getConditionalQuery()
	originalQuery := strings.Replace(query, "SELECT *", "SELECT COUNT(*)", 1)
	row := d.db.sql.QueryRow(originalQuery, args...)

	var count int64
	if err := row.Scan(&count); err != nil {
		return d.NilError(err)
	}
	return d.Push(lua.LNumber(count))
}

func (d *DB) exec(query string, args []interface{}) int {
	var result sql.Result
	var err error
	if d.tx != nil {
		result, err = d.tx.Exec(query, args...)
	} else {
		result, err = d.db.sql.Exec(query, args...)
	}
	if err != nil {
		return d.NilError(err)
	}
	LastInsertId, err := result.LastInsertId()
	if err != nil {
		return d.NilError(err)
	}
	RowsAffected, err := result.RowsAffected()
	if err != nil {
		return d.NilError(err)
	}
	rTable := d.Vm.NewTable()
	rTable.RawSetString("LastInsertId", lua.LNumber(LastInsertId))
	rTable.RawSetString("RowsAffected", lua.LNumber(RowsAffected))
	return d.Push(rTable)
}

func (d *DB) query(query string, args []interface{}, isRows bool) int {
	var rows *sql.Rows
	var err error
	if d.tx != nil {
		rows, err = d.tx.Query(query, args...)
	} else {
		rows, err = d.db.sql.Query(query, args...)
	}
	if err != nil {
		return d.NilError(err)
	}
	defer rows.Close()

	rTable, err := d.parseRows(rows, isRows)
	if err != nil {
		return d.NilError(err)
	}
	return d.Push(rTable)
}

func (d *DB) parseRows(rows *sql.Rows, isRows bool) (*lua.LTable, error) {
	lRows := d.Vm.NewTable()
	lCols := d.Vm.NewTable()

	if isRows {
		for rows.Next() {
			rowTable, columns, err := d.makeRow(rows)
			if err != nil {
				return nil, err
			}
			lCols.Append(columns)
			lRows.Append(rowTable)
		}
	} else {
		if !rows.Next() {
			return nil, sql.ErrNoRows
		}
		rowTable, columns, err := d.makeRow(rows)
		if err != nil {
			return nil, err
		}
		lRows = rowTable
		lCols = columns
	}

	rTable := d.Vm.NewTable()
	rTable.RawSetString("rows", lRows)
	rTable.RawSetString("columns", lCols)
	return rTable, nil
}

func (d *DB) makeRow(rows *sql.Rows) (*lua.LTable, *lua.LTable, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	clen := len(columns)
	values := make([]interface{}, clen)
	valuePtrs := make([]interface{}, clen)
	for i := range values {
		valuePtrs[i] = &values[i]
	}
	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, nil, err
	}

	lColumns := d.Vm.CreateTable(clen, 1)
	lRows := d.Vm.CreateTable(0, clen)

	for i, col := range columns {
		val := values[i]
		lColumns.Append(lua.LString(col))
		if bt, ok := val.([]byte); ok {
			lRows.RawSetString(col, lua.LString(bt))
		} else {
			lRows.RawSetString(col, util.ToLuaValue(val))
		}
	}
	return lRows, lColumns, nil
}

func (d *DB) getNativeQuery() (string, []interface{}) {
	query := d.Vm.CheckString(1)
	var args []interface{}
	for i := 2; i <= d.Vm.GetTop(); i++ {
		args = append(args, d.Vm.CheckAny(i))
	}
	return query, args
}

func (d *DB) getConditionalQuery() (string, []interface{}) {

	var builder strings.Builder

	if d.fields == "" {
		d.fields = "*"
	}
	builder.WriteString(fmt.Sprintf("SELECT %s FROM %s", d.fields, d.table))
	if d.where != "" {
		builder.WriteString(fmt.Sprintf(" WHERE %s", d.where))
	}
	if d.groupBy != "" {
		builder.WriteString(fmt.Sprintf(" GROUP BY %s", d.groupBy))
	}
	if d.having != "" {
		builder.WriteString(fmt.Sprintf(" HAVING %s", d.having))
	}
	if d.orderBy != "" {
		builder.WriteString(fmt.Sprintf(" ORDER BY %s", d.orderBy))
	}
	if d.limit > 0 {
		builder.WriteString(fmt.Sprintf(" LIMIT %d", d.limit))
	}
	query, args := builder.String(), d.args

	d.resetConditional()

	return query, args
}

func (d *DB) checkTable(t string) error {
	if d.table == "" {
		return fmt.Errorf("%v requires table name", t)
	}
	return nil
}

func (d *DB) checkWhere(t string) error {
	if d.where == "" {
		return fmt.Errorf("%v requires where name", t)
	}
	return nil
}
