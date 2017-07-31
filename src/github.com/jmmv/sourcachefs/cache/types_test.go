// Copyright 2017 Google Inc.
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
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type CachedFileInfoSuite struct {
	suite.Suite
}

func TestCachedFileInfo(t *testing.T) {
	suite.Run(t, new(CachedFileInfoSuite))
}

func (s *CachedFileInfoSuite) TestFile() {
	mtime := time.Date(2017, 6, 25, 17, 26, 30, 1234, time.Local)
	fi := newFileInfo("/some/long/name", 12345, 0644, mtime)
	s.Equal("name", fi.Name())
	s.Equal(int64(12345), fi.Size())
	s.Equal(os.FileMode(0644), fi.Mode())
	s.Equal(mtime, fi.ModTime())
	s.False(fi.IsDir())
	s.Nil(fi.Sys())
}

func (s *CachedFileInfoSuite) TestDir() {
	mtime := time.Date(2017, 6, 25, 17, 27, 45, 0, time.Local)
	fi := newFileInfo("../this/is/a/dir", 0, 0755|os.ModeDir, mtime)
	s.Equal("dir", fi.Name())
	s.Equal(int64(0), fi.Size())
	s.Equal(0755|os.ModeDir, fi.Mode())
	s.Equal(mtime, fi.ModTime())
	s.True(fi.IsDir())
	s.Nil(fi.Sys())
}
