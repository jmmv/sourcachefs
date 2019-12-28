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
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/jmmv/sourcachefs/internal/test"
)

type NewMetadataCacheSuite struct {
	test.SuiteWithTempDir
}

func TestNewMetadataCache(t *testing.T) {
	suite.Run(t, new(NewMetadataCacheSuite))
}

func (s *NewMetadataCacheSuite) TestOk() {
	path := filepath.Join(s.TempDir(), "metadata.db")
	cache, err := NewMetadataCache(path)
	s.Require().NoError(err)
	defer cache.Close()
	defer os.Remove(path)
}

func (s *NewMetadataCacheSuite) TestFailMissingDirectory() {
	missingDir := filepath.Join(s.TempDir(), "subdir/metadata.db")

	_, err := NewMetadataCache(missingDir)
	s.Require().Error(err)

	_, err = os.Stat(missingDir)
	s.True(os.IsNotExist(err), "cache directory was created but should not have been")
}

type MetadataCacheSuite struct {
	test.SuiteWithTempDir
	path  string
	cache *MetadataCache
}

func TestMetadataCache(t *testing.T) {
	suite.Run(t, new(MetadataCacheSuite))
}

func (s *MetadataCacheSuite) SetupTest() {
	s.path = filepath.Join(s.TempDir(), "metadata.db")
	cache, err := NewMetadataCache(s.path)
	s.Require().NoError(err)
	s.cache = cache
}

func (s *MetadataCacheSuite) TearDownTest() {
	defer os.Remove(s.path)
	err := s.cache.Close()
	s.Require().NoError(err)
}

func (s *MetadataCacheSuite) checkedGetFullEntry(path string, expectedType string) *CachedRow {
	row, err := s.cache.GetFullEntry(path)
	s.Require().NoError(err)
	s.Equal(expectedType, row.TypeName)
	return row
}

func (s *MetadataCacheSuite) TestPutNewDirectory() {
	inode, err := s.cache.PutNewWithType("/some/path", os.ModeDir)
	s.Require().NoError(err)
	s.Equal(inode, s.checkedGetFullEntry("/some/path", "directory").Inode)

	_, err = s.cache.PutNewWithType("/some/path", 0)
	s.Require().Error(err)
	s.checkedGetFullEntry("/some/path", "directory")
}

func (s *MetadataCacheSuite) TestPutNewFile() {
	inode, err := s.cache.PutNewWithType("/some/path", 0)
	s.Require().NoError(err)
	s.Equal(inode, s.checkedGetFullEntry("/some/path", "file").Inode)

	_, err = s.cache.PutNewWithType("/some/path", os.ModeDir)
	s.Require().Error(err)
	s.checkedGetFullEntry("/some/path", "file")
}

func (s *MetadataCacheSuite) TestPutNewSymlink() {
	inode, err := s.cache.PutNewWithType("/some/path", os.ModeSymlink)
	s.Require().NoError(err)
	s.Equal(inode, s.checkedGetFullEntry("/some/path", "symlink").Inode)

	_, err = s.cache.PutNewWithType("/some/path", 0)
	s.Require().Error(err)
	s.checkedGetFullEntry("/some/path", "symlink")
}

func (s *MetadataCacheSuite) TestPutNewNoEntry() {
	inode, err := s.cache.PutNewNoEntry("/some/path")
	s.Require().NoError(err)
	s.Equal(inode, s.checkedGetFullEntry("/some/path", "noentry").Inode)

	_, err = s.cache.PutNewNoEntry("/some/path")
	s.Require().Error(err)
	s.checkedGetFullEntry("/some/path", "noentry")
}

func (s *MetadataCacheSuite) TestPutNewOrUpdate() {
	s.Require().NoError(s.cache.PutNewOrUpdateNoEntry("/path1"))
	s.Require().NoError(s.cache.PutNewOrUpdateWithType("/path2", os.ModeDir))

	inode1 := s.checkedGetFullEntry("/path1", "noentry").Inode
	inode2 := s.checkedGetFullEntry("/path2", "directory").Inode

	s.Require().NoError(s.cache.PutNewOrUpdateWithType("/path1", os.ModeDir))
	s.Require().NoError(s.cache.PutNewOrUpdateNoEntry("/path2"))

	s.Equal(inode1, s.checkedGetFullEntry("/path1", "directory").Inode)
	s.Equal(inode2, s.checkedGetFullEntry("/path2", "noentry").Inode)

	s.Require().NoError(s.cache.PutNewOrUpdateNoEntry("/path1"))
	s.Require().NoError(s.cache.PutNewOrUpdateWithType("/path2", os.ModeSymlink))

	s.Equal(inode1, s.checkedGetFullEntry("/path1", "noentry").Inode)
	s.Equal(inode2, s.checkedGetFullEntry("/path2", "symlink").Inode)
}

func (s *MetadataCacheSuite) TestPutFileInfoAndGet() {
	inode, err := s.cache.PutNewWithType("/path", 0)
	s.Require().NoError(err)
	// Strip monotonic clock reading for comparisons with require.Equal.
	// See https://github.com/stretchr/testify/issues/502 for details.
	fileInfo := newFileInfo("/path", 123, 0, time.Now().Round(0))
	s.Require().NoError(s.cache.PutFileInfo(inode, fileInfo))

	row := s.checkedGetFullEntry("/path", "file")
	s.Equal(fileInfo, *row.FileInfo)
}

func (s *MetadataCacheSuite) TestPutContentHashAndGet() {
	inode, err := s.cache.PutNewWithType("/path", 0)
	s.Require().NoError(err)

	s.Require().NoError(s.cache.PutContentHash(inode, "123456"))

	row := s.checkedGetFullEntry("/path", "file")
	s.Equal(Key("123456"), *row.ContentHash)
}

func (s *MetadataCacheSuite) TestPutSymlinkTargetAndGet() {
	inode, err := s.cache.PutNewWithType("/path", os.ModeSymlink)
	s.Require().NoError(err)

	s.Require().NoError(s.cache.PutSymlinkTarget(inode, "/other/path"))

	row := s.checkedGetFullEntry("/path", "symlink")
	s.Equal("/other/path", *row.Target)
}

func (s *MetadataCacheSuite) TestPutDirEntriesAndGet() {
	inode, err := s.cache.PutNewWithType("/path", os.ModeDir)
	s.Require().NoError(err)

	entries := []os.FileInfo{
		newFileInfo("1", 123, 0, time.Now()),
		newFileInfo("2", 0, os.ModeDir, time.Now()),
		newFileInfo("3", 0, os.ModeSymlink, time.Now()),
	}

	s.Require().NoError(s.cache.PutDirEntries(inode, "/path", entries))

	inode1 := s.checkedGetFullEntry("/path/1", "file").Inode
	inode2 := s.checkedGetFullEntry("/path/2", "directory").Inode
	inode3 := s.checkedGetFullEntry("/path/3", "symlink").Inode

	expEntries := map[string]OneDirEntry{
		"1": {Valid: true, Inode: inode1, ModeType: 0},
		"2": {Valid: true, Inode: inode2, ModeType: os.ModeDir},
		"3": {Valid: true, Inode: inode3, ModeType: os.ModeSymlink},
	}

	row := s.checkedGetFullEntry("/path", "directory")
	s.Equal(len(entries), len(row.DirEntries))
	s.Equal(expEntries["1"], row.DirEntries["1"])
	s.Equal(expEntries["2"], row.DirEntries["2"])
	s.Equal(expEntries["3"], row.DirEntries["3"])

	readEntries := make(map[string]OneDirEntry)
	s.Require().NoError(s.cache.GetDirEntries(inode, &readEntries))
	s.Equal(expEntries, readEntries)
}
