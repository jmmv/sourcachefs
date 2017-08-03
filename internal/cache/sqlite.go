// Copyright 2016 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may not
// use this file except in compliance with the License.  You may obtain a copy
// of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  See the
// License for the specific language governing permissions and limitations
// under the License.

package cache

import (
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"github.com/golang/glog"
	"github.com/mattn/go-sqlite3"
)

const (
	// Baseline delay to back off on contention.
	baseDelay = 100 * time.Millisecond

	// Maximum increment in the delay each time an operation is retried.
	backoffMaxDelay = 500 * time.Millisecond
)

// sqliteDB extends an SQLite connection to wrap all SQL methods with versions they retry
// automatically when there is contention.
type sqliteDB struct {
	*sql.DB
}

// initDatabase sets up the database using the provided schema DDL.  The schema is provided as a
// list of DDL statements.
func initDatabase(db *sql.DB, schemaDDL []string) error {
	for _, statement := range schemaDDL {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// newSqliteDb instantiates a new SQLite database.  If the database given in path does not exist, it
// is created.  No matter what, the list of statements given in schemaDDL is executed so they should
// be idempotent.  It is OK to include PRAGMAs in the schemaDDL because they will be run each time
// the database is open.
func newSqliteDb(path string, schemaDDL []string) (*sqliteDB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}

	if err = initDatabase(db, schemaDDL); err != nil {
		db.Close()
		return nil, err
	}

	return &sqliteDB{db}, nil
}

// waitOrErr sleeps for a bit if the given error indicates contention in the database, or else
// returns the real error.  The id parameter is used for logging purposes and should identify the
// key of the database that was affected by the contention.  The delayAccumulator parameter is
// incremented by a random amount if the call decides to sleep; subsequent calls to waitOrErr should
// use the same delayAccumulator parameter.
func waitOrErr(id string, err error, delayAccumulator *time.Duration) error {
	switch typedErr := err.(type) {
	case sqlite3.Error:
		switch typedErr.Code {
		case sqlite3.ErrBusy:
			increment := rand.Int63n(int64(backoffMaxDelay))
			*delayAccumulator = *delayAccumulator + time.Duration(increment)

			glog.ErrorDepth(1, fmt.Sprintf("retrying in %s for %s: %s\n", delayAccumulator.String(), id, err))
			time.Sleep(*delayAccumulator)
			return nil
		}
		return err
	default:
		return err
	}
}

// RetriableStmt extends an sql.Stmt with functions to retry queries.
type RetriableStmt struct {
	*sql.Stmt
}

// RetriablePrepare prepares a new statement that supports automatic retry of queries during
// contention.
func (db *sqliteDB) RetriablePrepare(query string) (*RetriableStmt, error) {
	stmt, err := db.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &RetriableStmt{stmt}, nil
}

// RetryQuery wraps sql.DB.Query to automatically retry on database contention.
func (stmt *RetriableStmt) RetryQuery(id string, args ...interface{}) (
	*sql.Rows, error) {
	delay := baseDelay
retry:
	rows, err := stmt.Query(args...)
	if err != nil {
		err = waitOrErr(id, err, &delay)
		if err != nil {
			return nil, err
		}
		goto retry
	}
	return rows, nil
}

// RetriableRow wraps an sql.Row to be used as a helper for RetryQueryRow.
type RetriableRow struct {
	id  string
	row *sql.Row
}

// Scan wraps sql.DB.Row.Scan to automatically retry on database contention.
func (row *RetriableRow) Scan(dest ...interface{}) error {
	delay := baseDelay
retry:
	err := row.row.Scan(dest...)
	if err != nil {
		err = waitOrErr(row.id, err, &delay)
		if err != nil {
			return err
		}
		goto retry
	}
	return nil
}

// RetryQueryRow wraps sql.DB.QueryRow to automatically retry on database contention.
func (stmt *RetriableStmt) RetryQueryRow(id string, args ...interface{}) *RetriableRow {
	return &RetriableRow{
		id:  id,
		row: stmt.QueryRow(args...),
	}
}

// RetryExec wraps sql.DB.Exec to automatically retry on database contention.
func (stmt *RetriableStmt) RetryExec(id string, args ...interface{}) (
	sql.Result, error) {
	delay := baseDelay
retry:
	result, err := stmt.Exec(args...)
	if err != nil {
		err = waitOrErr(id, err, &delay)
		if err != nil {
			return nil, err
		}
		goto retry
	}
	return result, nil
}
