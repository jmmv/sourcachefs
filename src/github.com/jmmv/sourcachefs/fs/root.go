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
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/golang/glog"

	"github.com/jmmv/sourcachefs/cache"
	"github.com/jmmv/sourcachefs/real"
	"github.com/jmmv/sourcachefs/stats"
)

type globalState struct {
	syscalls        real.Syscalls
	cache           *cache.Cache
	stats           stats.Updater
	cachedPathRegex *regexp.Regexp
}

func newNodeForEntry(globals *globalState, parentPath string, parentBelowPath string, row *cache.CachedRow, name string) (fs.Node, error) {
	path := filepath.Join(parentPath, name)
	belowPath := filepath.Join(parentBelowPath, name)

	direct := !globals.cachedPathRegex.MatchString(path)

	switch row.TypeName {
	case "directory":
		return &Dir{
			globals:   globals,
			path:      path,
			belowPath: belowPath,
			inode:     row.Inode,
			direct:    direct,
			data: &lazyDirData{
				fileInfo:   row.FileInfo,
				dirEntries: row.DirEntries,
				rows:       make(map[string]*cache.CachedRow),
			},
		}, nil
	case "file":
		return &File{
			globals:   globals,
			path:      path,
			belowPath: belowPath,
			inode:     row.Inode,
			direct:    direct,
			data: &lazyFileData{
				fileInfo:    row.FileInfo,
				contentHash: row.ContentHash,
			},
		}, nil
	case "noentry":
		return nil, fuse.ENOENT
	case "symlink":
		return &Symlink{
			globals:   globals,
			path:      path,
			belowPath: belowPath,
			inode:     row.Inode,
			direct:    direct,
			data: &lazySymlinkData{
				fileInfo: row.FileInfo,
				target:   row.Target,
			},
		}, nil
	default:
		glog.Fatalf("unknown entry type")
		return nil, nil // golang/go #10037
	}
}

func newNodeForOneDirEntry(globals *globalState, parentPath string, parentBelowPath string, dirEntry *cache.OneDirEntry, name string) (fs.Node, error) {
	path := filepath.Join(parentPath, name)
	belowPath := filepath.Join(parentBelowPath, name)

	direct := !globals.cachedPathRegex.MatchString(path)

	switch dirEntry.ModeType {
	case os.ModeDir:
		return &Dir{
			globals:   globals,
			path:      path,
			belowPath: belowPath,
			inode:     dirEntry.Inode,
			direct:    direct,
			data: &lazyDirData{
				fileInfo:   nil,
				dirEntries: nil,
				rows:       make(map[string]*cache.CachedRow),
			},
		}, nil
	case 0:
		return &File{
			globals:   globals,
			path:      path,
			belowPath: belowPath,
			inode:     dirEntry.Inode,
			direct:    direct,
			data: &lazyFileData{
				fileInfo:    nil,
				contentHash: nil,
			},
		}, nil
	case os.ModeSymlink:
		return &Symlink{
			globals:   globals,
			path:      path,
			belowPath: belowPath,
			inode:     dirEntry.Inode,
			direct:    direct,
			data: &lazySymlinkData{
				fileInfo: nil,
				target:   nil,
			},
		}, nil
	default:
		glog.Fatalf("unknown entry type")
		return nil, nil // golang/go #10037
	}
}

func commonAttr(globals *globalState, inode uint64, direct bool, belowPath string, fileInfo **os.FileInfo, a *fuse.Attr) error {
	cached := *fileInfo != nil

	if direct {
		globals.stats.AccountDirect(stats.AttrFuseOp)

		rawFileInfo, err := globals.syscalls.Lstat(stats.RemoteDomain, belowPath)
		switch {
		case err != nil && cached:
			// Reuse cached row.

		case err != nil && !cached:
			glog.Errorf("%s: %s", belowPath, err)
			return err

		case err == nil:
			err = globals.cache.Metadata.PutFileInfo(inode, rawFileInfo)
			if err != nil {
				glog.Errorf("%s: %s", belowPath, err)
				return err
			}

			*fileInfo = &rawFileInfo
		}
	} else if !cached {
		globals.stats.AccountCacheMiss(stats.AttrFuseOp)

		rawFileInfo, err := globals.syscalls.Lstat(stats.RemoteDomain, belowPath)
		if err != nil {
			glog.Errorf("%s: %s", belowPath, err)
			return err
		}

		err = globals.cache.Metadata.PutFileInfo(inode, rawFileInfo)
		if err != nil {
			glog.Errorf("%s: %s", belowPath, err)
			return err
		}

		*fileInfo = &rawFileInfo
	} else {
		globals.stats.AccountCacheHit(stats.AttrFuseOp)
	}

	a.Inode = inode
	a.Mode = (**fileInfo).Mode()
	a.Size = uint64((**fileInfo).Size())
	a.Mtime = (**fileInfo).ModTime()
	return nil
}

func putAndGetRow(globals *globalState, path string, modeType os.FileMode) (
	*cache.CachedRow, error) {
	direct := !globals.cachedPathRegex.MatchString(path)

	cacheRow, err := globals.cache.Metadata.GetFullEntry(path)
	if err != nil {
		glog.Errorf("%s: %s", path, err)
		return nil, err
	}
	cached := cacheRow != nil

	if direct {
		err := globals.cache.Metadata.PutNewOrUpdateWithType(path, modeType)
		if err != nil {
			glog.Errorf("%s: %s", path, err)
			return nil, err
		}
	} else if !cached {
		_, err := globals.cache.Metadata.PutNewWithType(path, modeType)
		if err != nil {
			glog.Errorf("%s: %s", path, err)
			return nil, err
		}
	}

	cacheRow, err = globals.cache.Metadata.GetFullEntry(path)
	if err != nil {
		glog.Errorf("%s: %s", path, err)
		return nil, err
	}
	if cacheRow == nil {
		glog.Errorf("couldn't reload entry after put for %s", path)
		return nil, err
	}

	return cacheRow, nil
}

// unmountOnSignal captures termination signals and unmounts the filesystem.
//
// This allows the main program to exit the FUSE serving loop cleanly and avoids
// leaking a mount point without the backing FUSE server.
func unmountOnSignal(mountPoint string, caught chan<- os.Signal) {
	wait := make(chan os.Signal, 1)
	signal.Notify(wait, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	caught <- <-wait
	signal.Reset()
	err := fuse.Unmount(mountPoint)
	if err != nil {
		glog.Warningf("unmounting filesystem failed with error: %v", err)
	}
}

// Loop mounts the file system and starts serving.
func Loop(remotedir string, sc real.Syscalls, c *cache.Cache, s stats.Updater, mountpoint string, cachedPathRegex *regexp.Regexp) error {
	connection, err := fuse.Mount(
		mountpoint,
		fuse.FSName("sourcachefs"),
		fuse.Subtype("sourcachefs"),
		//fuse.LocalVolume(),
		fuse.VolumeName("sourcachefs"),
		fuse.MaxReadahead(8*1024*1024),
		//fuse.Async(),
		fuse.AsyncRead(),
		fuse.ReadOnly(),
		// Avoid Access() calls.  Replace with an Access handler that returns ENOSYS.
		fuse.DefaultPermissions(),
		// Because we prefetch files on Open, such operations can be very slow.  Bump the timeout,
		// though maybe this is too much and we should be doing something different instead of
		// prefetching.
		fuse.DaemonTimeout("600"),
	)
	if err != nil {
		return err
	}
	defer connection.Close()

	caughtSignal := make(chan os.Signal, 1)
	go unmountOnSignal(mountpoint, caughtSignal)

	globals := &globalState{
		syscalls:        sc,
		cache:           c,
		stats:           s,
		cachedPathRegex: cachedPathRegex,
	}

	cacheRow, err := putAndGetRow(globals, "/", os.ModeDir)
	if err != nil {
		return err
	}

	rootDir, err := newNodeForEntry(globals, "/", remotedir, cacheRow, "/")
	if err != nil {
		return err
	}
	root := FS{
		rootNode: rootDir,
	}

	err = fs.Serve(connection, root)
	if err != nil {
		return err
	}

	<-connection.Ready
	if connection.MountError != nil {
		return connection.MountError
	}

	// If we reach this point, the FUSE serve loop has terminated either because the user unmounted
	// the file system or we have received a signal.  The signal handler also unmounted the file
	// system, so either way the file system is unmounted at this point.
	select {
	case signal := <-caughtSignal:
		// Redeliver the signal to ourselves (the handler was reset to the default) to exit with
		// the correct exit code.
		proc, err := os.FindProcess(os.Getpid())
		if err != nil {
			return err
		}
		proc.Signal(signal)

		// Block indefinitely until the signal is delivered.
		for {
			time.Sleep(1 * time.Second)
		}
	default:
		return nil
	}
}

// FS represents the file system.
type FS struct {
	rootNode fs.Node
}

// Root returns the root node for the file system.
func (fs FS) Root() (fs.Node, error) {
	return fs.rootNode, nil
}
