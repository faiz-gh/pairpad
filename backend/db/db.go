package db

import (
	"database/sql"
	"log"

	"github.com/faiz-gh/go-postgresql-starter/config"
	_ "github.com/lib/pq"
)

func NewPostgreSQLStorage() (*sql.DB, error) {
	db, err := sql.Open("postgres", config.ENV.DBUrl)
	if err != nil {
		log.Fatal(err)
	}

	return db, nil
}
