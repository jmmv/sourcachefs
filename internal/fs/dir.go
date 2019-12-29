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

package fs

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/golang/glog"

	"github.com/jmmv/sourcachefs/internal/cache"
	"github.com/jmmv/sourcachefs/internal/stats"
)

type lazyDirData struct {
	fileInfo   *os.FileInfo
	didReadDir bool
	dirEntries map[string]cache.OneDirEntry
	rows       map[string]*cache.CachedRow
}

// Dir implements both Node and Handle for the root directory.
type Dir struct {
	globals   *globalState
	path      string
	belowPath string
	lock      sync.Mutex
	inode     uint64
	direct    bool
	data      *lazyDirData
}

var _ fs.Node = (*Dir)(nil)

// Attr queries the properties of the directory.
func (dir *Dir) Attr(ctx context.Context, a *fuse.Attr) error {
	dir.lock.Lock()
	defer dir.lock.Unlock()

	return commonAttr(dir.globals, dir.inode, dir.direct, dir.belowPath, &dir.data.fileInfo, a)
}

var _ fs.NodeStringLookuper = (*Dir)(nil)

// Lookup searches for a file entry.
func (dir *Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	dir.lock.Lock()
	defer dir.lock.Unlock()

	path := filepath.Join(dir.path, name)

	if !dir.direct { // Fast path in case we already know about the entry.
		cachedRow, ok := dir.data.rows[name]
		if ok {
			return newNodeForEntry(dir.globals, dir.path, dir.belowPath, cachedRow, name)
		}
	}

	cachedRow, err := dir.globals.cache.Metadata.GetFullEntry(path)
	if err != nil {
		glog.Errorf("%s: %s", dir.path, err)
		return nil, err
	}
	cached := cachedRow != nil

	if dir.direct {
		dir.globals.stats.AccountDirect(stats.LookupFuseOp)

		belowPath := filepath.Join(dir.belowPath, name)
		fileInfo, err := dir.globals.syscalls.Lstat(stats.RemoteDomain, belowPath)
		switch {
		case err == nil:
			err = dir.globals.cache.Metadata.PutNewOrUpdateWithType(path, fileInfo.Mode()&os.ModeType)
			if err != nil {
				glog.Errorf("%s: %s", path, err)
				return nil, err
			}

		case err != nil && cached:
			// Reuse cached row, queried later.

		case err != nil && !cached && os.IsNotExist(err):
			err := dir.globals.cache.Metadata.PutNewOrUpdateNoEntry(path)
			if err != nil {
				glog.Errorf("%s: %s", path, err)
				return nil, err
			}
			return nil, fuse.ENOENT

		case err != nil && !cached && !os.IsNotExist(err):
			glog.Errorf("%s: %s", path, err)
			return nil, err
		}

		cachedRow, err = dir.globals.cache.Metadata.GetFullEntry(path)
		if err != nil {
			glog.Errorf("%s: %s", dir.path, err)
			return nil, err
		}
		if cachedRow == nil {
			glog.Errorf("couldn't reload entry after put for %s", dir.path)
		}
	} else if !cached {
		dir.globals.stats.AccountCacheMiss(stats.LookupFuseOp)
		belowPath := filepath.Join(dir.belowPath, name)

		fileInfo, err := dir.globals.syscalls.Lstat(stats.RemoteDomain, belowPath)
		switch {
		case err != nil && !os.IsNotExist(err):
			glog.Errorf("%s: %s", dir.path, err)
			return nil, err
		case err != nil:
			_, err := dir.globals.cache.Metadata.PutNewNoEntry(path)
			if err != nil {
				glog.Errorf("%s: %s", path, err)
				return nil, err
			}
			return nil, fuse.ENOENT
		default:
			// Fallthrough.
		}

		_, err = dir.globals.cache.Metadata.PutNewWithType(path, fileInfo.Mode()&os.ModeType)
		if err != nil {
			glog.Errorf("%s: %s", path, err)
			return nil, err
		}

		cachedRow, err = dir.globals.cache.Metadata.GetFullEntry(path)
		if err != nil {
			glog.Errorf("%s: %s", dir.path, err)
			return nil, err
		}
		if cachedRow == nil {
			glog.Errorf("couldn't reload entry after put for %s", dir.path)
		}
	} else {
		dir.globals.stats.AccountCacheHit(stats.LookupFuseOp)
	}
	dir.data.rows[name] = cachedRow

	return newNodeForEntry(dir.globals, dir.path, dir.belowPath, cachedRow, name)
}

var _ fs.HandleReadDirAller = (*Dir)(nil)

// ReadDirAll returns all entries in a directory.
// TODO(jmmv): Decouple handle implementation from the Dir node.
func (dir *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	dir.lock.Lock()
	defer dir.lock.Unlock()

	cached := dir.data.dirEntries != nil

	if dir.direct {
		dir.globals.stats.AccountDirect(stats.ReaddirFuseOp)

		f, err := dir.globals.syscalls.Open(stats.RemoteDomain, dir.belowPath)
		switch {
		case err == nil:
			defer f.Close()

			belowEntries, err := f.Readdir(0)
			if err != nil {
				glog.Errorf("%s: %s", dir.path, err)
				return nil, err
			}

			err = dir.globals.cache.Metadata.PutDirEntries(dir.inode, dir.path, belowEntries)
			if err != nil {
				glog.Errorf("%s: %s", dir.path, err)
				return nil, err
			}

		case err != nil && cached:
			// Reuse dir.data.dirEntries

		case err != nil && !cached:
			glog.Errorf("%s: %s", dir.path, err)
			return nil, err
		}

		// TODO: Suboptimal probably, but we need the inode numbers.  Maybe
		// GetDirEntries should return a set of cache rows instead, but then
		// we need to merge those with any partially-known results created by
		// lookup.
		entries := make(map[string]cache.OneDirEntry)
		err = dir.globals.cache.Metadata.GetDirEntries(dir.inode, &entries)
		if err != nil {
			glog.Errorf("%s: %s", dir.path, err)
			return nil, err
		}
		dir.data.dirEntries = entries
	} else if !cached {
		dir.globals.stats.AccountCacheMiss(stats.ReaddirFuseOp)

		f, err := dir.globals.syscalls.Open(stats.RemoteDomain, dir.belowPath)
		if err != nil {
			glog.Errorf("%s: %s", dir.path, err)
			return nil, err
		}
		defer f.Close()

		belowEntries, err := f.Readdir(0)
		if err != nil {
			glog.Errorf("%s: %s", dir.path, err)
			return nil, err
		}

		err = dir.globals.cache.Metadata.PutDirEntries(dir.inode, dir.path, belowEntries)
		if err != nil {
			glog.Errorf("%s: %s", dir.path, err)
			return nil, err
		}

		// TODO: Suboptimal probably, but we need the inode numbers.  Maybe
		// GetDirEntries should return a set of cache rows instead, but then
		// we need to merge those with any partially-known results created by
		// lookup.
		entries := make(map[string]cache.OneDirEntry)
		err = dir.globals.cache.Metadata.GetDirEntries(dir.inode, &entries)
		if err != nil {
			glog.Errorf("%s: %s", dir.path, err)
			return nil, err
		}
		dir.data.dirEntries = entries
	} else {
		dir.globals.stats.AccountCacheHit(stats.ReaddirFuseOp)
	}

	dirents := make([]fuse.Dirent, 0, len(dir.data.dirEntries))
	for basename, dirEntry := range dir.data.dirEntries {
		if !dirEntry.Valid {
			continue
		}

		var de fuse.Dirent
		de.Inode = dirEntry.Inode
		de.Name = basename
		switch dirEntry.ModeType & os.ModeType {
		case 0:
			de.Type = fuse.DT_File
		case os.ModeDir:
			de.Type = fuse.DT_Dir
		case os.ModeSymlink:
			de.Type = fuse.DT_Link
		default:
			panic("unknown type")
		}
		dirents = append(dirents, de)
	}
	return dirents, nil
}
