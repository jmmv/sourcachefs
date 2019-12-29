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
	"sync"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/golang/glog"

	"github.com/jmmv/sourcachefs/internal/cache"
	"github.com/jmmv/sourcachefs/internal/real"
	"github.com/jmmv/sourcachefs/internal/stats"
)

type openFile struct {
	globals *globalState
	path    string
	reader  real.FileReader
}

var _ fs.Handle = (*openFile)(nil)

var _ fs.HandleReader = (*openFile)(nil)

func (file *openFile) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	file.globals.stats.AccountCacheHit(stats.ReadFuseOp)
	resp.Data = resp.Data[:req.Size]
	n, err := file.reader.ReadAt(resp.Data, req.Offset)
	if err != nil {
		glog.Errorf("read error: %s", err)
	}
	resp.Data = resp.Data[:n]
	return err
}

var _ fs.HandleReleaser = (*openFile)(nil)

func (file *openFile) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	if file.reader == nil {
		glog.Warningf("double-close of already-closed file handle")
		return nil
	}
	err := file.reader.Close()
	if err != nil {
		glog.Errorf("close error: %s", err)
		return err
	}
	file.reader = nil
	return nil
}

type lazyFileData struct {
	fileInfo    *os.FileInfo
	contentHash *cache.Key
}

// File implements both Node and Handle for the hello file.
type File struct {
	globals   *globalState
	path      string
	belowPath string
	lock      sync.Mutex
	inode     uint64
	direct    bool
	data      *lazyFileData
}

var _ fs.Node = (*File)(nil)

// Attr queries the file properties of the file.
func (file *File) Attr(ctx context.Context, a *fuse.Attr) error {
	file.lock.Lock()
	defer file.lock.Unlock()

	return commonAttr(file.globals, file.inode, file.direct, file.belowPath, &file.data.fileInfo, a)
}

var _ fs.NodeOpener = (*File)(nil)

// Open obtains a file handle for the open request if the file exists.
func (file *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	file.lock.Lock()
	defer file.lock.Unlock()

	cached := file.data.contentHash != nil

	if file.direct {
		file.globals.stats.AccountDirect(stats.OpenFuseOp)

		glog.Infof("fetching remote file %s", file.belowPath)
		key, err := file.globals.cache.Content.PutContents(file.belowPath)
		switch {
		case err == nil:
			err = file.globals.cache.Metadata.PutContentHash(file.inode, key)
			if err != nil {
				glog.Errorf("%s: %s", file.path, err)
				return nil, err
			}

			file.data.contentHash = &key

		case err != nil && cached:
			// Reuse file.data.contentHash

		case err != nil && !cached:
			glog.Errorf("%s: %s", file.path, err)
			return nil, err
		}
	} else if !cached {
		file.globals.stats.AccountCacheMiss(stats.OpenFuseOp)

		glog.Infof("fetching remote file %s", file.belowPath)
		key, err := file.globals.cache.Content.PutContents(file.belowPath)
		if err != nil {
			glog.Errorf("%s: %s", file.path, err)
			return nil, err
		}

		err = file.globals.cache.Metadata.PutContentHash(file.inode, key)
		if err != nil {
			glog.Errorf("%s: %s", file.path, err)
			return nil, err
		}

		file.data.contentHash = &key
	} else {
		file.globals.stats.AccountCacheHit(stats.OpenFuseOp)
	}

	path, err := file.globals.cache.Content.GetFileForContents(cache.Key(*file.data.contentHash))
	if err != nil {
		glog.Errorf("%s: %s", file.path, err)
		return nil, err
	}

	input, err := file.globals.syscalls.Open(stats.LocalDomain, path)
	if err != nil {
		glog.Errorf("%s: %s", file.path, err)
		return nil, err
	}
	return &openFile{
		globals: file.globals,
		path:    file.path,
		reader:  input,
	}, nil
}
