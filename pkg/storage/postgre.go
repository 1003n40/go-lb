package storage

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"time"
)

type postgresDBConfig struct {
	address  string
	port     uint
	user     string
	password string
	dbname   string
}

func NewPostgresConfig(address string, port uint) (*postgresDBConfig, error) {
	user := os.Getenv("PG_USER")
	if user == "" {
		return nil, errors.New("postgres user not provided")
	}

	passwd := os.Getenv("PG_PASSWD")
	if passwd == "" {
		return nil, errors.New("postgres password not provided")
	}

	dbname := os.Getenv("PG_DBNAME")
	if dbname == "" {
		return nil, errors.New("postgres db name not provided")
	}

	return &postgresDBConfig{
		address:  address,
		port:     port,
		user:     user,
		password: passwd,
		dbname:   dbname,
	}, nil
}

type dbConnectionPool struct {
	*gorm.DB
}

func (c *postgresDBConfig) connectionString() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		c.address, c.port, c.user, c.password, c.dbname)
}

func GetConnectionPool(config postgresDBConfig) (*dbConnectionPool, error) {
	db, err := sql.Open("postgres", config.connectionString())
	if err != nil {
		return nil, err
	}

	db.SetMaxIdleConns(10)
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(time.Hour)

	if err = db.Ping(); err != nil {
		return nil, err
	}
	gorm, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	return &dbConnectionPool{gorm}, nil
}

func (p *dbConnectionPool) Close() error {
	db, err := p.DB.DB()
	if err != nil {
		return err
	}

	return db.Close()
}
