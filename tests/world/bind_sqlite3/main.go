package main

import (
	"fmt"

	"marmotworld/bind_sqlite3/sqlite3"
)

func main() {
	db, err := sqlite3.Open(":memory:")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()
	run := func(sql string) {
		st, _, err := db.PrepareV2(sql, -1)
		if err != nil {
			fmt.Println(err, db.Errmsg())
			return
		}
		for st.Step() == sqlite3.Row {
			for i := 0; i < st.ColumnCount(); i++ {
				fmt.Printf("%s=%s ", st.ColumnName(i), st.ColumnText(i))
			}
			fmt.Println()
		}
		st.Close()
	}
	run("create table t (id integer primary key, name text)")
	ins, _, _ := db.PrepareV2("insert into t (name) values ('a'), ('b'), ('c')", -1)
	fmt.Println(ins.Step() == sqlite3.Done, db.Changes(), db.LastInsertRowid())
	ins.Close()
	q, _, _ := db.PrepareV2("select id * ? from t where id > ?", -1)
	q.BindInt(1, 10)
	q.BindInt(2, 1)
	for q.Step() == sqlite3.Row {
		fmt.Print(q.ColumnInt(0), " ")
	}
	fmt.Println()
	q.Close()
	run("select name, length(name) as n from t order by id desc")
	_, _, err = db.PrepareV2("select * from missing", -1)
	fmt.Println(err, "|", db.Errmsg())
}
