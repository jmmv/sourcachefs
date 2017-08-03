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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
)

const (
	dirEntrySeparator      = "/"
	dirEntryFieldSeparator = "\x01"
)

var schemaDDL = []string{
	"PRAGMA cache_size = 10000",
	"PRAGMA journal_mode = OFF",
	"PRAGMA read_uncommitted = ON",
	"PRAGMA synchronous = OFF",

	`CREATE TABLE IF NOT EXISTS paths (
		path STRING PRIMARY KEY,
		type STRING NOT NULL,

		mode INTEGER,
		size INTEGER,
		mtime_nsec INTEGER,

		dirEntries STRING,
		contentHash STRING,

		target STRING
	)`,
}

// MetadataCache is a persistent database of the file system metadata for all known entries.
type MetadataCache struct {
	db *sqliteDB

	getDirEntriesStmt    *RetriableStmt
	getInodeStmt         *RetriableStmt
	getFullEntryStmt     *RetriableStmt
	updateEntryStmt      *RetriableStmt
	putNewEntryStmt      *RetriableStmt
	putFileInfoStmt      *RetriableStmt
	putContentHashStmt   *RetriableStmt
	putSymlinkTargetStmt *RetriableStmt
	putDirEntriesStmt    *RetriableStmt
}

// NewMetadataCache opens a new connection to the given database.
func NewMetadataCache(path string) (*MetadataCache, error) {
	db, err := newSqliteDb(path, schemaDDL)
	if err != nil {
		return nil, err
	}

	var stmtError error
	prepareStmt := func(query string) *RetriableStmt {
		if stmtError != nil {
			return nil
		}
		stmt, err := db.RetriablePrepare(query)
		if err != nil {
			stmtError = err
			return nil
		}
		return stmt
	}

	cache := &MetadataCache{
		db:                   db,
		getDirEntriesStmt:    prepareStmt("SELECT dirEntries FROM paths WHERE ROWID = ?"),
		getInodeStmt:         prepareStmt("SELECT ROWID FROM paths WHERE path = ?"),
		getFullEntryStmt:     prepareStmt("SELECT ROWID, type, mode, size, mtime_nsec, dirEntries, contentHash, target FROM paths WHERE path = ?"),
		updateEntryStmt:      prepareStmt("UPDATE paths SET type = ? WHERE path = ?"),
		putNewEntryStmt:      prepareStmt("INSERT INTO paths (path, type) VALUES (?, ?)"),
		putFileInfoStmt:      prepareStmt("UPDATE paths SET mode = ?, size = ?, mtime_nsec = ? WHERE ROWID = ?"),
		putContentHashStmt:   prepareStmt("UPDATE paths SET contentHash = ? WHERE ROWID = ?"),
		putSymlinkTargetStmt: prepareStmt("UPDATE paths SET target = ? WHERE ROWID = ?"),
		putDirEntriesStmt:    prepareStmt("UPDATE paths SET dirEntries = ? WHERE ROWID = ?"),
	}

	if stmtError != nil {
		db.Close()
		return nil, stmtError
	}

	return cache, nil
}

func (cache *MetadataCache) closeStmts() error {
	var firstError error
	closeStmt := func(stmt **RetriableStmt) {
		if *stmt == nil || firstError != nil {
			return
		}
		if err := (*stmt).Close(); err != nil {
			firstError = err
			return
		}
		*stmt = nil
	}
	closeStmt(&cache.putDirEntriesStmt)
	closeStmt(&cache.putSymlinkTargetStmt)
	closeStmt(&cache.putContentHashStmt)
	closeStmt(&cache.putFileInfoStmt)
	closeStmt(&cache.putNewEntryStmt)
	closeStmt(&cache.updateEntryStmt)
	closeStmt(&cache.getFullEntryStmt)
	closeStmt(&cache.getInodeStmt)
	closeStmt(&cache.getDirEntriesStmt)
	return firstError
}

// Close terminates the connection to the database.
func (cache *MetadataCache) Close() error {
	if err := cache.closeStmts(); err != nil {
		return err
	}
	return cache.db.Close()
}

func rowIDToInode(rowID int64) uint64 {
	if rowID <= 0 {
		panic("invalid ROWID")
	}
	return uint64(rowID)
}

func addDirEntry(dirEntries *map[string]OneDirEntry, inode uint64, basename string, typeName string) error {
	_, has := (*dirEntries)[basename]
	if has {
		return nil
	}

	dirEntry := OneDirEntry{
		Inode: inode,
	}
	switch typeName {
	case "directory":
		dirEntry.Valid = true
		dirEntry.ModeType = os.ModeDir
	case "file":
		dirEntry.Valid = true
		dirEntry.ModeType = 0
	case "noentry":
		dirEntry.Valid = false
	case "symlink":
		dirEntry.Valid = true
		dirEntry.ModeType = os.ModeSymlink
	default:
		return errors.New("Invalid type " + typeName)
	}
	(*dirEntries)[basename] = dirEntry
	return nil
}

// GetDirEntries reads the directory entries stored in an inode.
func (cache *MetadataCache) GetDirEntries(inode uint64, outEntries *map[string]OneDirEntry) error {
	var dirEntries sql.NullString
	err := cache.getDirEntriesStmt.RetryQueryRow("NO PATH", inode).Scan(&dirEntries)
	if err != nil {
		return err
	}
	if !dirEntries.Valid {
		return nil
	}
	return parseDirEntries(dirEntries.String, outEntries)
}

func parseDirEntries(all string, outEntries *map[string]OneDirEntry) error {
	if len(all) == 0 {
		return nil
	}
	for _, one := range strings.Split(all, dirEntrySeparator) {
		parts := strings.Split(one, dirEntryFieldSeparator)
		basename := parts[0]
		inode, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return err
		}
		typeName := parts[2]

		err = addDirEntry(outEntries, inode, basename, typeName)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetFullEntry reads the full metadata for a file.
func (cache *MetadataCache) GetFullEntry(path string) (*CachedRow, error) {
	var rowID int64
	var typeName string
	var mode sql.NullInt64
	var size sql.NullInt64
	var mtimeNsec sql.NullInt64
	var dirEntries sql.NullString
	var contentHash sql.NullString
	var target sql.NullString
	err := cache.getFullEntryStmt.RetryQueryRow(path, path).Scan(&rowID, &typeName, &mode, &size, &mtimeNsec, &dirEntries, &contentHash, &target)
	switch {
	case err == sql.ErrNoRows:
		return nil, nil
	case err != nil:
		return nil, err
	default:
		row := &CachedRow{
			Inode:    uint64(rowID),
			TypeName: typeName,
		}
		if mode.Valid || size.Valid || mtimeNsec.Valid {
			if !(mode.Valid && size.Valid && mtimeNsec.Valid) {
				glog.Fatal("incomplete fileInfo data in cache row")
			}
			mtime := time.Unix(0, mtimeNsec.Int64)
			fileInfo := newFileInfo(path, size.Int64, os.FileMode(mode.Int64), mtime)
			row.FileInfo = &fileInfo
		}
		if dirEntries.Valid {
			row.DirEntries = make(map[string]OneDirEntry)
			err := parseDirEntries(dirEntries.String, &row.DirEntries)
			if err != nil {
				return nil, err
			}
		}
		if contentHash.Valid {
			row.ContentHash = (*Key)(&contentHash.String)
		}
		if target.Valid {
			row.Target = &target.String
		}
		return row, nil
	}
}

func (cache *MetadataCache) resultToInode(result sql.Result) (uint64, error) {
	rowID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if rowID < 0 {
		glog.Fatal("got negative row ID; cannot use as inode number")
	}
	return uint64(rowID), nil
}

func (cache *MetadataCache) putNew(path string, typeName string) (uint64, error) {
	glog.Infof("putNew: path %s, type %s", path, typeName)

	result, err := cache.putNewEntryStmt.RetryExec(path, path, typeName)
	if err != nil {
		glog.Errorf("failed to put %s: %s", path, err)
		return 0, err
	}
	return cache.resultToInode(result)
}

func (cache *MetadataCache) putNewOrUpdate(path string, typeName string) error {
	glog.Infof("putNewOrUpdate: path %s, type %s", path, typeName)

	result, err := cache.updateEntryStmt.RetryExec(path, typeName, path)
	if err != nil {
		glog.Errorf("failed to update %s: %s", path, err)
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		glog.Errorf("failed to update %s: %s", path, err)
		return err
	}
	if count == 0 {
		_, err = cache.putNewEntryStmt.RetryExec(path, path, typeName)
		if err != nil {
			glog.Errorf("failed to put %s: %s", path, err)
			return err
		}
	} else if count == 1 {
		glog.Infof("putNewOrUpdate updated %s to %s", path, typeName)
	} else {
		panic("Updated more than one row")
	}
	return err
}

func sqlTypeName(modeType os.FileMode) string {
	if modeType & ^os.ModeType != 0 {
		panic("Input modeType contains permissions")
	}

	switch modeType {
	case 0:
		return "file"
	case os.ModeDir:
		return "directory"
	case os.ModeSymlink:
		return "symlink"
	default:
		panic("Unknown entry type")
	}
}

// PutNewWithType stores a new entry for a path given its file type.
func (cache *MetadataCache) PutNewWithType(path string, fileMode os.FileMode) (uint64, error) {
	if fileMode & ^os.ModeType != 0 {
		panic("fileMode contains more than just the file type")
	}
	typeName := sqlTypeName(fileMode & os.ModeType)
	return cache.putNew(path, typeName)
}

// PutNewNoEntry stores a new whiteout entry for the given path.
func (cache *MetadataCache) PutNewNoEntry(path string) (uint64, error) {
	return cache.putNew(path, "noentry")
}

// PutNewOrUpdateWithType stores a new entry or updates an existing one to match the given type.
func (cache *MetadataCache) PutNewOrUpdateWithType(path string, fileMode os.FileMode) error {
	if fileMode & ^os.ModeType != 0 {
		panic("fileMode contains more than just the file type")
	}
	typeName := sqlTypeName(fileMode & os.ModeType)
	return cache.putNewOrUpdate(path, typeName)
}

// PutNewOrUpdateNoEntry stores a new whiteout entry or updates an existing one.
func (cache *MetadataCache) PutNewOrUpdateNoEntry(path string) error {
	return cache.putNewOrUpdate(path, "noentry")
}

func checkUpdateAffectedOneRow(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		glog.WarningDepth(1, fmt.Sprintf("update affected %d rows; expected only 1", count))
		return nil
	}
	return nil
}

// PutFileInfo stores new file details for an existing inode.
func (cache *MetadataCache) PutFileInfo(inode uint64, fileInfo os.FileInfo) error {
	result, err := cache.putFileInfoStmt.RetryExec("NO PATH", fileInfo.Mode(), fileInfo.Size(), fileInfo.ModTime().UnixNano(), inode)
	if err != nil {
		return err
	}
	return checkUpdateAffectedOneRow(result)
}

// PutContentHash stores a new content hash for an existing inode.
func (cache *MetadataCache) PutContentHash(inode uint64, contentHash Key) error {
	result, err := cache.putContentHashStmt.RetryExec("NO PATH", string(contentHash), inode)
	if err != nil {
		return err
	}
	return checkUpdateAffectedOneRow(result)
}

// PutSymlinkTarget stores a new symlink target for an existing inode.
func (cache *MetadataCache) PutSymlinkTarget(inode uint64, target string) error {
	result, err := cache.putSymlinkTargetStmt.RetryExec("NO PATH", target, inode)
	if err != nil {
		return err
	}
	return checkUpdateAffectedOneRow(result)
}

// PutDirEntries stores a new set of directory entries for an existing inode.
func (cache *MetadataCache) PutDirEntries(inode uint64, path string, entries []os.FileInfo) error {
	formatted := make([]string, 0, len(entries))
	for _, dirEntry := range entries {
		entryPath := filepath.Join(path, dirEntry.Name())

		typeName := sqlTypeName(dirEntry.Mode() & os.ModeType)

		var rowID int64
		err := cache.getInodeStmt.RetryQueryRow(path, entryPath).Scan(&rowID)
		switch {
		case err == sql.ErrNoRows:
			result, err := cache.putNewEntryStmt.RetryExec(path, entryPath, typeName)
			if err != nil {
				glog.Errorf("failed to put %s: %s", path, err)
				return err
			}

			rowID, err = result.LastInsertId()
			if err != nil {
				glog.Errorf("failed to put %s: %s", path, err)
				return err
			}
		case err != nil:
			return err
		default:
		}
		inode := uint64(rowID)

		formatted = append(formatted, fmt.Sprintf("%s%s%d%s%s", dirEntry.Name(), dirEntryFieldSeparator, inode, dirEntryFieldSeparator, typeName))
	}

	result, err := cache.putDirEntriesStmt.RetryExec("NO PATH", strings.Join(formatted, dirEntrySeparator), inode)
	if err != nil {
		return err
	}
	return checkUpdateAffectedOneRow(result)
}
