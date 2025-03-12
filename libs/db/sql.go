package db

import (
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// SQL represents a database connection wrapper.
type SQL struct {
	sql    *sql.DB
	config dbConfig
	refs   int
	locker sync.Mutex
}

// dbConfig holds database configuration options.
type dbConfig struct {
	sharedMode   bool
	maxOpenConns int
	maxIdleConns int
	dsn          string
	driver       string
}

var (
	shared       = make(map[string]*SQL)
	sharedLocker sync.Mutex
)

// NewSQL creates a new SQL instance with the given configuration.
func NewSQL(config dbConfig) (*SQL, error) {

	if !isDriverSupported(config.driver) {
		return nil, fmt.Errorf("unsupported driver: %s", config.driver)
	}

	sharedLocker.Lock()
	defer sharedLocker.Unlock()

	if config.sharedMode {
		if existing, ok := shared[config.dsn]; ok {
			existing.refs++
			return existing, nil
		}
	}

	db, err := sql.Open(config.driver, config.dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err = db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if config.maxOpenConns > 0 {
		db.SetMaxOpenConns(config.maxOpenConns)
	}
	if config.maxIdleConns > 0 {
		db.SetMaxIdleConns(config.maxIdleConns)
	}

	sqlInstance := &SQL{sql: db, config: config, refs: 1}
	if config.sharedMode {
		shared[config.dsn] = sqlInstance
	}
	return sqlInstance, nil
}

// isDriverSupported checks if the given driver is supported.
func isDriverSupported(driver string) bool {
	for _, d := range sql.Drivers() {
		if d == driver {
			return true
		}
	}
	return false
}

// Close closes the database connection, respecting shared mode.
func (s *SQL) close() error {
	s.locker.Lock()
	defer s.locker.Unlock()

	if s.config.sharedMode {
		sharedLocker.Lock()
		defer sharedLocker.Unlock()

		s.refs--
		if s.refs > 0 {
			return nil
		}
		delete(shared, s.config.dsn)
	}
	return s.sql.Close()
}
