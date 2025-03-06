package libs

import (
	"database/sql"
	"fmt"
	"strings"

	"lug/util"

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
	limit   int
	args    []interface{}
}

func (d *DB) reset() {
	d.table = ""
	d.fields = ""
	d.where = ""
	d.groupBy = ""
	d.having = ""
	d.orderBy = ""
	d.limit = 0
	d.args = nil
}

func DbLoader(L *lua.LState) int {
	mod := util.GetModule(L)
	api := util.LGFunctions{"open": openDatabase}
	return mod.SetFuncs(api)
}

func openDatabase(L *lua.LState) int {
	Type := L.CheckString(1)
	dsn := L.CheckString(2)
	db, err := newDatabase(Type, dsn)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	mod := &DB{
		Module: *util.GetModule(L),
		db:     db,
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
	return mod.SetFuncs(api)
}

func newDatabase(Type, dsn string) (*sql.DB, error) {
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
		return nil, err
	}
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) Table(L *lua.LState) int {
	d.table = L.CheckString(1)
	return d.Self()
}

func (d *DB) Field(L *lua.LState) int {
	d.fields = L.CheckString(1)
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

func (d *DB) Close(L *lua.LState) int {
	err := d.db.Close()
	return d.Push(lua.LString(err.Error()))
}

func (d *DB) Query(L *lua.LState) int {
	query, args := d.getNativeQuery()
	return d.query(query, args, true)
}

func (d *DB) Select(L *lua.LState) int {
	query, args := d.getConditionalQuery()
	return d.query(query, args, true)
}

func (d *DB) Find(L *lua.LState) int {
	query, args := d.getConditionalQuery()
	return d.query(query, args, false)
}

func (d *DB) Exec(L *lua.LState) int {
	query, args := d.getNativeQuery()
	return d.exec(query, args)
}

func (d *DB) Insert(L *lua.LState) int {

	if err := d.checkTable("insert"); err != nil {
		return d.Error(err)
	}
	data := L.CheckTable(1)
	dLen := data.Len()

	keys := make([]string, 0, dLen)
	placeholders := make([]string, 0, dLen)
	values := make([]interface{}, 0, dLen)

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

	result, err := d.db.Exec(query, values...)
	if err != nil {
		return d.Error(err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return d.Error(err)
	}

	return d.Push(lua.LNumber(id))
}

func (d *DB) Update(L *lua.LState) int {

	if err := d.checkTable("update"); err != nil {
		return d.Error(err)
	}
	if err := d.checkWhere("update"); err != nil {
		return d.Error(err)
	}

	data := L.CheckTable(1)
	dLen := data.Len()

	sets := make([]string, 0, dLen)
	values := make([]interface{}, 0, dLen)

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
	if err := d.checkTable("update"); err != nil {
		return d.Error(err)
	}
	if err := d.checkWhere("update"); err != nil {
		return d.Error(err)
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", d.table, d.where)
	return d.exec(query, d.args)
}

func (d *DB) Count(L *lua.LState) int {
	query, args := d.getConditionalQuery()
	originalQuery := strings.Replace(query, "SELECT *", "SELECT COUNT(*)", 1)
	var count int64
	err := d.db.QueryRow(originalQuery, args...).Scan(&count)
	if err != nil {
		return d.Error(err)
	}
	return d.Push(lua.LNumber(count))
}

func (d *DB) exec(query string, args []interface{}) int {
	result, err := d.db.Exec(query, args...)
	if err != nil {
		return d.Error(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return d.Error(err)
	}
	return d.Push(lua.LNumber(count))
}

func (d *DB) query(query string, args []interface{}, isRows bool) int {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return d.Error(err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return d.Error(err)
	}

	var results *lua.LTable
	if isRows {
		results = d.Vm.NewTable()
		for rows.Next() {
			rowTable, err := d.makeRow(rows, columns)
			if err != nil {
				return d.Error(err)
			}
			results.Append(rowTable)
		}
	} else {
		if !rows.Next() {
			return d.Error(sql.ErrNoRows)
		}
		results, err = d.makeRow(rows, columns)
		if err != nil {
			return d.Error(err)
		}
	}
	return d.Push(results)
}

func (d *DB) makeRow(rows *sql.Rows, columns []string) (*lua.LTable, error) {
	cLen := len(columns)
	values := make([]interface{}, cLen)
	valuePtrs := make([]interface{}, cLen)
	for i := range values {
		valuePtrs[i] = &values[i]
	}
	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, err
	}
	result := d.Vm.NewTable()
	for i, col := range columns {
		val := values[i]
		if bt, ok := val.([]byte); ok {
			result.RawSetString(col, lua.LString(bt))
		} else {
			result.RawSetString(col, util.ToLuaValue(val))
		}
	}
	return result, nil
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

	d.reset()

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
