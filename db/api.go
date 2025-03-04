package db

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/geekip/lug/util"
	lua "github.com/yuin/gopher-lua"
)

func (d *DB) Table(L *lua.LState) int {
	d.table = L.CheckString(1)
	return d.This()
}

func (d *DB) Field(L *lua.LState) int {
	d.fields = L.CheckString(1)
	return d.This()
}

func (d *DB) Where(L *lua.LState) int {
	query, args := d.getNativeQuery()
	d.where = query
	d.args = append(d.args, args...)
	return d.This()
}

func (d *DB) Group(L *lua.LState) int {
	d.groupBy = L.CheckString(1)
	return d.This()
}

func (d *DB) Having(L *lua.LState) int {
	d.having = L.CheckString(1)
	return d.This()
}

func (d *DB) Order(L *lua.LState) int {
	d.orderBy = L.CheckString(1)
	return d.This()
}

func (d *DB) Limit(L *lua.LState) int {
	d.limit = strconv.Itoa(L.CheckInt(1))
	return d.This()
}

func (d *DB) Close(L *lua.LState) int {
	err := d.db.Close()
	return d.Push(lua.LString(err.Error()))
}

func (d *DB) Query(L *lua.LState) int {
	query, args := d.getNativeQuery()
	return d._query(query, args, true)
}

func (d *DB) Select(L *lua.LState) int {
	query, args := d.getConditionalQuery()
	return d._query(query, args, true)
}

func (d *DB) Find(L *lua.LState) int {
	query, args := d.getConditionalQuery()
	return d._query(query, args, false)
}

func (d *DB) Exec(L *lua.LState) int {
	query, args := d.getNativeQuery()
	return d._exec(query, args)
}

func (d *DB) Insert(L *lua.LState) int {
	data := L.CheckTable(1)
	dataLen := data.Len()

	keys := make([]string, 0, dataLen)
	placeholders := make([]string, 0, dataLen)
	values := make([]interface{}, 0, dataLen)

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
		return d.error(err.Error())
	}

	id, err := result.LastInsertId()
	if err != nil {
		return d.error(err.Error())
	}

	return d.Push(lua.LNumber(id))
}

func (d *DB) Update(L *lua.LState) int {
	data := L.CheckTable(1)
	dataLen := data.Len()
	if d.where == "" {
		return d.error("update requires WHERE clause")
	}
	sets := make([]string, 0, dataLen)
	values := make([]interface{}, 0, dataLen)

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
	return d._exec(query, values)
}

func (d *DB) Delete(L *lua.LState) int {
	if d.where == "" {
		return d.error("delete requires WHERE clause")
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", d.table, d.where)
	return d._exec(query, d.args)
}

func (d *DB) Count(L *lua.LState) int {
	query, args := d.getConditionalQuery()
	originalQuery := strings.Replace(query, "SELECT *", "SELECT COUNT(*)", 1)
	var count int64
	err := d.db.QueryRow(originalQuery, args...).Scan(&count)
	if err != nil {
		return d.error(err.Error())
	}
	return d.Push(lua.LNumber(count))
}

func (d *DB) _exec(query string, args []interface{}) int {
	result, err := d.db.Exec(query, args...)
	if err != nil {
		return d.error(err.Error())
	}

	count, err := result.RowsAffected()
	if err != nil {
		return d.error(err.Error())
	}
	return d.Push(lua.LNumber(count))
}

func (d *DB) _query(query string, args []interface{}, isAll bool) int {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return d.error(err.Error())
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return d.error(err.Error())
	}

	var results *lua.LTable
	if isAll {
		results = d.VmState.NewTable()
		for rows.Next() {
			rowTable, err := d.makeRow(rows, columns)
			if err != nil {
				return d.error(err.Error())
			}
			results.Append(rowTable)
		}
	} else {
		if !rows.Next() {
			return d.error(sql.ErrNoRows.Error())
		}
		results, err = d.makeRow(rows, columns)
		if err != nil {
			return d.error(err.Error())
		}
	}
	return d.Push(results)
}

func (d *DB) makeRow(rows *sql.Rows, columns []string) (*lua.LTable, error) {
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}
	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, err
	}
	result := d.VmState.NewTable()
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
	query := d.VmState.CheckString(1)
	argLen := d.VmState.GetTop()
	var args []interface{}
	if argLen > 1 {
		args = make([]interface{}, 0)
		for i := 2; i <= argLen; i++ {
			args = append(args, d.VmState.CheckAny(i))
		}
	}
	return query, args
}

func (d *DB) getConditionalQuery() (string, []interface{}) {
	if d.fields == "" {
		d.fields = "*"
	}
	query := fmt.Sprintf("SELECT %s FROM %s", d.fields, d.table)
	var args []interface{}

	if d.where != "" {
		query += " WHERE " + d.where
		args = append(args, d.args...)
	}
	if d.groupBy != "" {
		query += " GROUP BY " + d.groupBy
	}
	if d.having != "" {
		query += " HAVING " + d.having
	}
	if d.orderBy != "" {
		query += " ORDER BY " + d.orderBy
	}
	if d.limit != "" {
		query += " LIMIT " + d.limit
	}

	d.table = ""
	d.fields = ""
	d.where = ""
	d.groupBy = ""
	d.having = ""
	d.orderBy = ""
	d.limit = ""
	d.args = nil

	return query, args
}

func (d *DB) error(err string) int {
	d.VmState.Push(lua.LNil)
	d.VmState.Push(lua.LString(err))
	return 2
}
