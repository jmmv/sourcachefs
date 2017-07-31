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

	"github.com/golang/glog"

	"github.com/jmmv/sourcachefs/real"
	"github.com/jmmv/sourcachefs/stats"
)

// Cache implements the unified cache.
type Cache struct {
	Metadata *MetadataCache
	Content  *ContentCache
}

// NewCache instantantiates a new cache.
func NewCache(root string, syscalls real.Syscalls) (*Cache, error) {
	contentCache := NewContentCache(root, syscalls)

	metadataFile := filepath.Join(root, "metadata.db")
	metadataCache, err := NewMetadataCache(metadataFile)
	if err != nil {
		if err2 := syscalls.Remove(stats.LocalDomain, metadataFile); err2 != nil {
			glog.Warning("failed to delete metadata cache on error")
		}
		if err2 := syscalls.Remove(stats.LocalDomain, root); err2 != nil {
			glog.Warning("failed to delete cache directory")
		}
		return nil, err
	}

	return &Cache{
		Metadata: metadataCache,
		Content:  contentCache,
	}, nil
}

// Close closes the cache in a controller manner.
func (c *Cache) Close() error {
	return c.Metadata.Close()
}
