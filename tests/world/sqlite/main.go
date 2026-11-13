package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`create table t (id integer primary key, name text, score real)`); err != nil {
		log.Fatal(err)
	}
	tx, _ := db.Begin()
	st, _ := tx.Prepare(`insert into t (name, score) values (?, ?)`)
	for i := 0; i < 1000; i++ {
		st.Exec(fmt.Sprintf("n%03d", i), float64(i)*1.5)
	}
	st.Close()
	tx.Commit()
	var n int
	var sum float64
	db.QueryRow(`select count(*), sum(score) from t`).Scan(&n, &sum)
	fmt.Println(n, sum)
	rows, _ := db.Query(`select name, score from t where id % 250 = 0 order by id`)
	for rows.Next() {
		var name string
		var s float64
		rows.Scan(&name, &s)
		fmt.Println(name, s)
	}
	rows.Close()
	var v string
	db.QueryRow(`select sqlite_version()`).Scan(&v)
	fmt.Println(len(v) > 0)
	_, err = db.Exec(`select * from missing`)
	fmt.Println(err)
}
