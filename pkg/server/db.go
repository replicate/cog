package server

import (
	"context"
	"database/sql"
	"fmt"

	"golang.org/x/oauth2"
	_ "github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/dialers/postgres"
	"github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/proxy"
	"github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/certs"

	"github.com/replicate/modelserver/pkg/global"
)

type DB struct {
	db *sql.DB
}

func NewDB() (*DB, error) {
	var err error
	db := new(DB)
	connString := fmt.Sprintf("host=%s:%s:%s user=postgres dbname=%s password=%s sslmode=disable", global.GCPProject, global.CloudSQLRegion, global.CloudSQLInstance, global.CloudSQLDB, global.CloudSQLPassword)

	proxy.InitWithClient(&proxy.Client{
		Port: 3307,
		Certs: certs.NewCertSourceOpts(
			oauth2.NewClient(context.Background(), global.TokenSource),
			certs.RemoteOpts{
				IgnoreRegion: false,
				UserAgent: "custom cloud_sql_proxy version >= 1.10",
				TokenSource: global.TokenSource,
			},
		),
	})

	db.db, err = sql.Open("cloudsqlpostgres", connString)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) InsertModel(name string, hash string, gcsPath string, cpuImage string, gpuImage string) error {
	sqlStatement := `
INSERT INTO models (name, hash, gcs_path, cpu_image, gpu_image)
VALUES ($1, $2, $3, $4)`
	if _, err := db.db.Exec(sqlStatement, name, hash, gcsPath, cpuImage, gpuImage); err != nil {
		return err
	}
	return nil
}

func (db *DB) Close() {
	db.db.Close()
}
