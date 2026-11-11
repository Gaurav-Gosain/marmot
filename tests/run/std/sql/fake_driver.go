package main

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
)

type rows struct {
	cols []string
	data [][]driver.Value
	i    int
}

func (r *rows) Columns() []string { return r.cols }
func (r *rows) Close() error      { return nil }
func (r *rows) Next(dest []driver.Value) error {
	if r.i >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.i])
	r.i++
	return nil
}

type drv struct{ log *[]string }
type conn struct{ log *[]string }
type stmt struct {
	q   string
	log *[]string
}
type tx struct{ log *[]string }

func (d drv) Open(string) (driver.Conn, error)       { return conn{d.log}, nil }
func (c conn) Prepare(q string) (driver.Stmt, error) { return stmt{q, c.log}, nil }
func (c conn) Close() error                          { return nil }
func (c conn) Begin() (driver.Tx, error)             { *c.log = append(*c.log, "begin"); return tx{c.log}, nil }
func (t tx) Commit() error                           { *t.log = append(*t.log, "commit"); return nil }
func (t tx) Rollback() error                         { *t.log = append(*t.log, "rollback"); return nil }
func (s stmt) Close() error                          { return nil }
func (s stmt) NumInput() int                         { return -1 }
func (s stmt) Exec(args []driver.Value) (driver.Result, error) {
	*s.log = append(*s.log, fmt.Sprint("exec ", s.q, args))
	if s.q == "fail" {
		return nil, errors.New("exec failed")
	}
	return driver.RowsAffected(len(args)), nil
}
func (s stmt) Query(args []driver.Value) (driver.Rows, error) {
	*s.log = append(*s.log, fmt.Sprint("query ", s.q, args))
	return &rows{[]string{"id", "name", "score", "note"}, [][]driver.Value{{int64(1), "ann", 1.5, nil}, {int64(2), []byte("bob"), 2.5, "n"}, {int64(3), "cy", int64(7), "z"}}, 0}, nil
}

func main() {
	var log []string
	sql.Register("fake", drv{&log})
	db, err := sql.Open("fake", "dsn")
	fmt.Println(err, db.Ping())
	rs, err := db.Query("select * from t where a = ?", 5, "x")
	fmt.Println(err)
	cols, _ := rs.Columns()
	fmt.Println(cols)
	for rs.Next() {
		var id int
		var name string
		var score float64
		var note sql.NullString
		fmt.Println(rs.Scan(&id, &name, &score, &note), id, name, score, note)
	}
	fmt.Println(rs.Err(), rs.Close())
	var n int
	fmt.Println(db.QueryRow("count").Scan(&n), n)
	var s string
	var f float32
	var b bool
	fmt.Println(db.QueryRow("x").Scan(&n, &s, &f, &b))
	res, err := db.Exec("insert", 1, 2, 3)
	ra, _ := res.RowsAffected()
	fmt.Println(err, ra)
	_, err = db.Exec("fail")
	fmt.Println(err)
	t, _ := db.Begin()
	t.Exec("in tx")
	fmt.Println(t.Commit(), t.Commit())
	t2, _ := db.Begin()
	fmt.Println(t2.Rollback())
	for _, l := range log {
		fmt.Println(l)
	}
	db.Close()
	fmt.Println(db.Ping() != nil)
}
