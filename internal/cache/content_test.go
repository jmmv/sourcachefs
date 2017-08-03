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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmmv/sourcachefs/internal/real"
	"github.com/jmmv/sourcachefs/internal/stats"
	"github.com/jmmv/sourcachefs/internal/test"
	"github.com/stretchr/testify/suite"
)

func newTemporaryContentCache(s *suite.Suite, dir string) *ContentCache {
	stats := stats.NewStats()
	cache := NewContentCache(dir, real.NewSyscalls(stats))
	s.T().Logf("Initialized content cache: %s", dir)
	return cache
}

type PutAndGetSuite struct {
	test.SuiteWithTempDir
}

func TestContentCache(t *testing.T) {
	suite.Run(t, new(PutAndGetSuite))
}

func (s *PutAndGetSuite) TestPutAndGet() {
	cache := newTemporaryContentCache(&s.Suite, filepath.Join(s.TempDir(), "cache"))
	s.NoError(os.Mkdir(cache.Root(), 0755))

	path1 := filepath.Join(s.TempDir(), "file1")
	s.NoError(ioutil.WriteFile(path1, []byte("Some text\n"), 0666))

	key, err := cache.PutContents(path1)
	s.NoError(err)

	expKey := Key("3c825ca59d58209eae5924221497780c")
	s.Equal(expKey, key)
	s.NoError(test.CheckFileExists(filepath.Join(cache.Root(), "3c", string(key))))

	found, err := cache.HasKey(key)
	s.NoError(err)
	s.True(found)

	cached, err := cache.GetFileForContents(key)
	s.NoError(err)
	s.Equal(string(cached), filepath.Join(cache.Root(), "3c", string(key)))
}
