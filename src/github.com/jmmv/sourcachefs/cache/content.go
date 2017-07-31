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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/jmmv/sourcachefs/real"
	"github.com/jmmv/sourcachefs/stats"
)

// ContentCache implements the content cache.
type ContentCache struct {
	root     string
	syscalls real.Syscalls
}

// Key represents a key in the cache.  Keys are file digests.
type Key string

// NewContentCache instantiates a new content cache.
func NewContentCache(root string, syscalls real.Syscalls) *ContentCache {
	return &ContentCache{
		root:     root,
		syscalls: syscalls,
	}
}

// Root returns the path to the root of the cache.
func (cache *ContentCache) Root() string {
	return cache.root
}

func (cache *ContentCache) bucketForKey(key Key) string {
	return string(key)[0:2]
}

func (cache *ContentCache) pathForKey(key Key) string {
	bucket := cache.bucketForKey(key)
	return filepath.Join(cache.root, bucket, string(key))
}

// GetFileForContents returns the path to file containing the contents for the key.
func (cache *ContentCache) GetFileForContents(key Key) (string, error) {
	// TODO(jmmv): Have an internal hasKey() that takes the joined path so that we don't do it
	// twice: once inside HasKey and once below.
	found, err := cache.HasKey(key)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("attempted to get non-existent key %s", key)
	}
	return cache.pathForKey(key), nil
}

// HasKey checks if the given key is in the cache.
func (cache *ContentCache) HasKey(key Key) (bool, error) {
	path := cache.pathForKey(key)
	_, err := cache.syscalls.Lstat(stats.LocalDomain, path)
	if err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

func copy2(dst1 io.Writer, dst2 io.Writer, src io.Reader) (written int64, err error) {
	buf := make([]byte, 32*1024)
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst1.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}

			dst2.Write(buf[0:nr])
			// TODO
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			err = er
			break
		}
	}
	return written, err
}

// PutContents stores the given file in the cache and returns the key.
func (cache *ContentCache) PutContents(path string) (Key, error) {
	input, err := cache.syscalls.Open(stats.RemoteDomain, path)
	if err != nil {
		return "", fmt.Errorf("failed to open %s: %s", path, err)
	}
	defer input.Close()

	temp, err := ioutil.TempFile(cache.root, "partial")
	if err != nil {
		return "", fmt.Errorf("cannot create temporary file: %s", err)
	}
	defer temp.Close()
	defer cache.syscalls.Remove(stats.LocalDomain, temp.Name())

	h := md5.New()
	_, err = copy2(temp, h, input)
	if err != nil {
		return "", fmt.Errorf("failed to fetch %s: %s", path, err)
	}
	input.Close()
	temp.Close()

	key := Key(hex.EncodeToString(h.Sum(nil)))

	found, err := cache.HasKey(key)
	if err != nil {
		return "", fmt.Errorf("failed to check for existence: %s", err)
	}
	if found {
		// TODO: Is this nice?
		return key, nil
	}

	bucket := cache.bucketForKey(key)
	if err = cache.syscalls.Mkdir(stats.LocalDomain, filepath.Join(cache.root, bucket), 0755); err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("failed to create bucket %s: %s", bucket, err)
	}

	err = cache.syscalls.Rename(stats.LocalDomain, temp.Name(), cache.pathForKey(key))
	if err != nil {
		return "", err
	}

	return key, nil
}
