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
	"os"
	"path/filepath"
	"time"
)

// OneDirEntry represents a single directory entry.
type OneDirEntry struct {
	Valid    bool
	Inode    uint64
	ModeType os.FileMode
}

// CachedRow contains all the data for an inode as known by the cache.
type CachedRow struct {
	Inode       uint64
	TypeName    string
	FileInfo    *os.FileInfo
	DidReadDir  bool
	DirEntries  map[string]OneDirEntry
	ContentHash *Key
	Target      *string
}

// cachedFileInfo exposes an all in-memory os.FileInfo implementation with
// data retrieved from the database.
type cachedFileInfo struct {
	basename string
	size     int64
	mode     os.FileMode
	mtime    time.Time
}

var _ os.FileInfo = (*cachedFileInfo)(nil)

// newFileInfo instantiates a new os.FileInfo with explicit values for each
// of the publicly available fields.
func newFileInfo(path string, size int64, mode os.FileMode, mtime time.Time) os.FileInfo {
	return &cachedFileInfo{
		basename: filepath.Base(path),
		size:     size,
		mode:     mode,
		mtime:    mtime,
	}
}

func (fi *cachedFileInfo) Name() string {
	return fi.basename
}

func (fi *cachedFileInfo) Size() int64 {
	return fi.size
}

func (fi *cachedFileInfo) Mode() os.FileMode {
	return fi.mode
}

func (fi *cachedFileInfo) ModTime() time.Time {
	return fi.mtime
}

func (fi *cachedFileInfo) IsDir() bool {
	return fi.mode.IsDir()
}

func (fi *cachedFileInfo) Sys() interface{} {
	return nil
}
