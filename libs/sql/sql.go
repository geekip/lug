package sql

import (
	"context"
	"database/sql"
	"fmt"
	"lug/util"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

type Sql struct {
	sql     *SQL
	tx      *sql.Tx
	table   string
	fields  string
	where   string
	groupBy string
	having  string
	orderBy string
	limit   int
	offset  int
	args    []interface{}
	api     *lua.LTable
}

func (s *Sql) resetConditional() {
	s.table = ""
	s.fields = ""
	s.where = ""
	s.groupBy = ""
	s.having = ""
	s.orderBy = ""
	s.limit = 0
	s.offset = 0
	s.args = nil
}

func Loader(L *lua.LState) int {
	api := util.SetMethods(L, util.Methods{
		"open": open,
	})
	return util.Push(L, api)
}

func extendMethods(s *Sql) util.Methods {
	return util.Methods{
		"table":    s.Table,
		"fields":   s.Fields,
		"where":    s.Where,
		"group":    s.Group,
		"having":   s.Having,
		"order":    s.Order,
		"limit":    s.Limit,
		"offset":   s.Offset,
		"query":    s.Query,
		"fetchAll": s.FetchAll,
		"fetch":    s.Fetch,
		"exec":     s.Exec,
		"insert":   s.Insert,
		"update":   s.Update,
		"delete":   s.Delete,
		"count":    s.Count,
	}
}

func open(L *lua.LState) int {
	config := getConfig(L)
	db, err := NewSQL(*config)
	if err != nil {
		return util.NilError(L, err)
	}

	instance := &Sql{
		sql: db,
	}

	api := util.SetMethods(L, extendMethods(instance), util.Methods{
		"transaction": instance.Transaction,
		"close":       instance.Close,
	})
	instance.api = api
	return util.Push(L, api)
}

func (s *Sql) Transaction(L *lua.LState) int {
	config := getTxOptions(L)
	ctx, cancel := context.WithTimeout(context.Background(), config.timeout)
	defer cancel()

	tx, err := s.sql.instance.BeginTx(ctx, config.options)
	if err != nil {
		return util.Error(L, err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		} else if err != nil {
			tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	instance := &Sql{
		tx:  tx,
		sql: s.sql,
	}

	methods := extendMethods(instance)
	api := util.SetMethods(L, methods, util.Methods{
		"rollback": instance.Rollback,
		"commit":   instance.Commit,
	})
	instance.api = api

	if err := util.CallLua(L, config.handler, api); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (s *Sql) Rollback(L *lua.LState) int {
	if err := s.tx.Rollback(); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (s *Sql) Commit(L *lua.LState) int {
	if err := s.tx.Commit(); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (s *Sql) Close(L *lua.LState) int {
	if err := s.sql.close(); err != nil {
		return util.Error(L, err)
	}
	return 0
}

func (s *Sql) Table(L *lua.LState) int {
	s.table = L.CheckString(1)
	return util.Push(L, s.api)
}

func (s *Sql) Fields(L *lua.LState) int {
	top := L.GetTop()
	fields := make([]string, 0, top)
	for i := 1; i <= top; i++ {
		fields = append(fields, L.CheckString(i))
	}
	s.fields = strings.Join(fields, ",")
	return util.Push(L, s.api)
}

func (s *Sql) Where(L *lua.LState) int {
	query, args := s.getNativeQuery(L)
	s.where = query
	s.args = append(s.args, args...)
	return util.Push(L, s.api)
}

func (s *Sql) Group(L *lua.LState) int {
	s.groupBy = L.CheckString(1)
	return util.Push(L, s.api)
}

func (s *Sql) Having(L *lua.LState) int {
	s.having = L.CheckString(1)
	return util.Push(L, s.api)
}

func (s *Sql) Order(L *lua.LState) int {
	s.orderBy = L.CheckString(1)
	return util.Push(L, s.api)
}

func (s *Sql) Limit(L *lua.LState) int {
	s.limit = L.CheckInt(1)
	return util.Push(L, s.api)
}

func (s *Sql) Offset(L *lua.LState) int {
	s.offset = L.CheckInt(1)
	return util.Push(L, s.api)
}

func (s *Sql) Query(L *lua.LState) int {
	query, args := s.getNativeQuery(L)
	return s.query(L, query, args, true)
}

func (s *Sql) FetchAll(L *lua.LState) int {
	query, args := s.getConditionalQuery()
	return s.query(L, query, args, true)
}

func (s *Sql) Fetch(L *lua.LState) int {
	s.limit = 1
	s.offset = 0
	query, args := s.getConditionalQuery()
	return s.query(L, query, args, false)
}

func (s *Sql) Exec(L *lua.LState) int {
	query, args := s.getNativeQuery(L)
	return s.exec(L, query, args)
}

func (s *Sql) Insert(L *lua.LState) int {
	if err := s.checkTable("insert"); err != nil {
		return util.NilError(L, err)
	}
	columns, values := s.processTableData(L)
	placeholders := make([]string, len(columns))
	for i := 0; i < len(columns); i++ {
		placeholders[i] = "?"
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		s.table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)
	return s.exec(L, query, values)
}

func (s *Sql) Update(L *lua.LState) int {
	if err := s.checkTable("update"); err != nil {
		return util.NilError(L, err)
	}
	if err := s.checkWhere("update"); err != nil {
		return util.NilError(L, err)
	}
	columns, values := s.processTableData(L)
	sets := make([]string, len(columns))
	for i, col := range columns {
		sets[i] = fmt.Sprintf("%s = ?", col)
	}
	values = append(values, s.args...)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		s.table,
		strings.Join(sets, ", "),
		s.where,
	)
	return s.exec(L, query, values)
}

func (s *Sql) processTableData(L *lua.LState) (columns []string, values []interface{}) {
	L.CheckTable(1).ForEach(func(lk, lv lua.LValue) {
		columns = append(columns, lk.String())
		values = append(values, lv)
	})
	return
}

func (s *Sql) Delete(L *lua.LState) int {
	if err := s.checkTable("delete"); err != nil {
		return util.NilError(L, err)
	}
	if err := s.checkWhere("delete"); err != nil {
		return util.NilError(L, err)
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", s.table, s.where)
	return s.exec(L, query, s.args)
}

func (s *Sql) Count(L *lua.LState) int {
	var builder strings.Builder
	builder.WriteString("SELECT count(*) FROM ")
	builder.WriteString(s.table)

	if s.where != "" {
		builder.WriteString(" WHERE ")
		builder.WriteString(s.where)
	}

	query := builder.String()
	row := s.sql.instance.QueryRow(query, s.args...)

	var count int64
	if err := row.Scan(&count); err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lua.LNumber(count))
}

func (s *Sql) exec(L *lua.LState, query string, args []interface{}) int {
	var result sql.Result
	var err error
	if s.tx != nil {
		result, err = s.tx.Exec(query, args...)
	} else {
		result, err = s.sql.instance.Exec(query, args...)
	}
	if err != nil {
		return util.NilError(L, err)
	}
	LastInsertId, err := result.LastInsertId()
	if err != nil {
		return util.NilError(L, err)
	}
	RowsAffected, err := result.RowsAffected()
	if err != nil {
		return util.NilError(L, err)
	}
	rTable := L.NewTable()
	rTable.RawSetString("lastInsertId", lua.LNumber(LastInsertId))
	rTable.RawSetString("rowsAffected", lua.LNumber(RowsAffected))
	return util.Push(L, rTable)
}

func (s *Sql) query(L *lua.LState, query string, args []interface{}, isRows bool) int {
	var rows *sql.Rows
	var err error
	if s.tx != nil {
		rows, err = s.tx.Query(query, args...)
	} else {
		rows, err = s.sql.instance.Query(query, args...)
	}
	if err != nil {
		return util.NilError(L, err)
	}
	defer rows.Close()

	lrows, err := s.parseRows(L, rows, isRows)
	if err != nil {
		return util.NilError(L, err)
	}
	return util.Push(L, lrows)
}

func (s *Sql) parseRows(L *lua.LState, rows *sql.Rows, isRows bool) (*lua.LTable, error) {
	lRows := L.NewTable()
	if isRows {
		for rows.Next() {
			rowTable, err := s.makeRow(L, rows)
			if err != nil {
				return nil, err
			}
			lRows.Append(rowTable)
		}
	} else {
		if !rows.Next() {
			return nil, sql.ErrNoRows
		}
		rowTable, err := s.makeRow(L, rows)
		if err != nil {
			return nil, err
		}
		lRows = rowTable
	}
	return lRows, nil
}

func (s *Sql) makeRow(L *lua.LState, rows *sql.Rows) (*lua.LTable, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	clen := len(columns)
	values := make([]interface{}, clen)
	valuePtrs := make([]interface{}, clen)
	for i := range values {
		valuePtrs[i] = &values[i]
	}
	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, err
	}

	lRows := L.CreateTable(0, clen)

	for i, col := range columns {
		val := values[i]
		// lColumns.Append(lua.LString(col))
		if bt, ok := val.([]byte); ok {
			lRows.RawSetString(col, lua.LString(bt))
		} else {
			lRows.RawSetString(col, util.ToLuaValue(val))
		}
	}
	return lRows, nil
}

func (s *Sql) getNativeQuery(L *lua.LState) (string, []interface{}) {
	query := L.CheckString(1)
	var args []interface{}
	for i := 2; i <= L.GetTop(); i++ {
		args = append(args, L.CheckAny(i))
	}
	return query, args
}

func (s *Sql) getConditionalQuery() (string, []interface{}) {
	var builder strings.Builder
	fields := s.fields
	if fields == "" {
		fields = "*"
	}
	builder.WriteString("SELECT ")
	builder.WriteString(fields)
	builder.WriteString(" FROM ")
	builder.WriteString(s.table)

	conditions := []struct {
		keyword string
		field   string
	}{
		{"WHERE", s.where},
		{"GROUP BY", s.groupBy},
		{"HAVING", s.having},
		{"ORDER BY", s.orderBy},
	}
	for _, c := range conditions {
		if c.field != "" {
			builder.WriteString(" ")
			builder.WriteString(c.keyword)
			builder.WriteString(" ")
			builder.WriteString(c.field)
		}
	}
	if s.limit > 0 {
		builder.WriteString(" LIMIT ")
		builder.WriteString(strconv.Itoa(s.limit))
	}
	if s.offset > 0 {
		builder.WriteString(" OFFSET ")
		builder.WriteString(strconv.Itoa(s.offset))
	}
	query, args := builder.String(), s.args
	s.resetConditional()
	return query, args
}

func (s *Sql) checkTable(t string) error {
	if s.table == "" {
		return fmt.Errorf("%v requires table name", t)
	}
	return nil
}

func (s *Sql) checkWhere(t string) error {
	if s.where == "" {
		return fmt.Errorf("%v requires where name", t)
	}
	return nil
}
