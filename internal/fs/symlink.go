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

	"github.com/jmmv/sourcachefs/internal/stats"
)

type lazySymlinkData struct {
	fileInfo *os.FileInfo
	target   *string
}

// Symlink implements both Node and Handle for the hello file.
type Symlink struct {
	globals   *globalState
	path      string
	belowPath string
	lock      sync.Mutex
	inode     uint64
	direct    bool
	data      *lazySymlinkData
}

var _ fs.Node = (*Symlink)(nil)

// Attr queries the file properties of the file.
func (symlink *Symlink) Attr(ctx context.Context, a *fuse.Attr) error {
	symlink.lock.Lock()
	defer symlink.lock.Unlock()

	return commonAttr(symlink.globals, symlink.inode, symlink.direct, symlink.belowPath, &symlink.data.fileInfo, a)
}

var _ fs.NodeReadlinker = (*Symlink)(nil)

// Readlink retrieves the target of the symlink.
func (symlink *Symlink) Readlink(ctx context.Context, req *fuse.ReadlinkRequest) (string, error) {
	symlink.lock.Lock()
	defer symlink.lock.Unlock()

	cached := symlink.data.target != nil

	if symlink.direct {
		symlink.globals.stats.AccountDirect(stats.ReadlinkFuseOp)

		target, err := symlink.globals.syscalls.Readlink(stats.RemoteDomain, symlink.belowPath)
		switch {
		case err == nil:
			err = symlink.globals.cache.Metadata.PutSymlinkTarget(symlink.inode, target)
			if err != nil {
				glog.Errorf("%s: %s", symlink.path, err)
				return "", err
			}

			symlink.data.target = &target

		case err != nil && cached:
			// Reuse symlink.data.target

		case err != nil && !cached:
			glog.Errorf("%s: %s", symlink.path, err)
			return "", err
		}
	} else if !cached {
		symlink.globals.stats.AccountCacheMiss(stats.ReadlinkFuseOp)

		target, err := symlink.globals.syscalls.Readlink(stats.RemoteDomain, symlink.belowPath)
		if err != nil {
			glog.Errorf("%s: %s", symlink.path, err)
			return "", err
		}

		err = symlink.globals.cache.Metadata.PutSymlinkTarget(symlink.inode, target)
		if err != nil {
			glog.Errorf("%s: %s", symlink.path, err)
			return "", err
		}

		symlink.data.target = &target
	} else {
		symlink.globals.stats.AccountCacheHit(stats.ReadlinkFuseOp)
	}

	return *symlink.data.target, nil
}
