package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/dialers/postgres"
)

const (
	cloudSQLConnectionTimeout  = 10 * time.Second
)

func NewGoogleCloudSQLDatabase(host string, user string, password string, dbName string) (*PostgresDatabase, error) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s sslmode=disable",
		host,
		user,
		password,
		dbName)
	db, err := sql.Open("cloudsqlpostgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to Google Cloud SQL: %w", err)
	}
	postgres := &PostgresDatabase{db: db}
	if err := postgres.ping(); err != nil {
		return nil, err
	}
	return postgres, nil
}
