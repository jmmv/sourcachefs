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
	"path/filepath"
	"testing"

	"github.com/jmmv/sourcachefs/test"
	"github.com/stretchr/testify/suite"
)

// newTestSqliteDb creates or opens an SQLite database and fails the test if
// the connection cannot be established.
func newTestSqliteDb(s *suite.Suite, path string, schemaDDL []string) *sqliteDB {
	db, err := newSqliteDb(path, schemaDDL)
	s.NoError(err)
	return db
}

type NewSqliteDbSuite struct {
	test.SuiteWithTempDir
}

func TestNewSqliteDbSuite(t *testing.T) {
	suite.Run(t, new(NewSqliteDbSuite))
}

func (s *NewSqliteDbSuite) TestSchemaDDLIsExecuted() {
	schemaDDL := []string{
		"CREATE TABLE first (a STRING)",
		"INSERT INTO first VALUES ('foo')",
	}
	db := newTestSqliteDb(&s.Suite, ":memory:", schemaDDL)
	defer db.Close()

	var value string
	s.NoError(db.QueryRow("SELECT a FROM first").Scan(&value))
	s.Equal("foo", value)
}

func (s *NewSqliteDbSuite) TestSchemaDDLIsExecutedOnEachOpen() {
	dbPath := filepath.Join(s.TempDir(), "test.db")

	schemaDDL := []string{
		"CREATE TABLE IF NOT EXISTS first (a STRING)",
		"INSERT INTO first VALUES ('foo')",
	}
	db := newTestSqliteDb(&s.Suite, dbPath, schemaDDL)
	db.Close()
	db = newTestSqliteDb(&s.Suite, dbPath, schemaDDL)
	defer db.Close()

	var count int
	query := "SELECT COUNT(a) FROM first"
	s.NoError(db.QueryRow(query).Scan(&count))
	s.Equal(2, count)
}

type RetriablePrepareSuite struct {
	test.SuiteWithTempDir
}

func TestRetriablePrepareSuite(t *testing.T) {
	suite.Run(t, new(RetriablePrepareSuite))
}

func (s *RetriablePrepareSuite) TestRetryExec() {
	// We cannot use an in-memory database in this test: SQLite3 doesn't
	// seem to like concurrent accesses to such a database even when the
	// database is opened in "full mutex" mode.
	dbPath := filepath.Join(s.TempDir(), "test.db")

	schemaDDL := []string{
		"CREATE TABLE foo (a INTEGER)",
		"INSERT INTO foo VALUES (500)",
	}
	db := newTestSqliteDb(&s.Suite, dbPath, schemaDDL)
	defer db.Close()

	stmt, err := db.RetriablePrepare("UPDATE foo SET a = a + 1")
	s.NoError(err)
	defer stmt.Close()

	tries := 100
	done := make(chan bool, tries)
	for i := 0; i < tries; i++ {
		go func() {
			_, err := stmt.RetryExec("test")
			s.NoError(err)
			done <- true
		}()
	}
	for i := 0; i < tries; i++ {
		<-done
	}

	var value int
	s.NoError(db.QueryRow("SELECT a FROM foo").Scan(&value))
	s.Equal(500+tries, value)
}
