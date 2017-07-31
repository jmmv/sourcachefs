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
	"path/filepath"
	"testing"

	"github.com/jmmv/sourcachefs/real"
	"github.com/jmmv/sourcachefs/stats"
	"github.com/jmmv/sourcachefs/test"
	"github.com/stretchr/testify/suite"
)

type NewCacheSuite struct {
	test.SuiteWithTempDir
}

func TestNewCache(t *testing.T) {
	suite.Run(t, new(NewCacheSuite))
}

func (s *NewCacheSuite) TestOk() {
	stats := stats.NewStats()
	cache, err := NewCache(s.TempDir(), real.NewSyscalls(stats))
	s.NoError(err)
	defer cache.Close()

	s.NotNil(cache.Metadata)
	s.NotNil(cache.Content)
}

func (s *NewCacheSuite) TestFailMissingDirectory() {
	missingDir := filepath.Join(s.TempDir(), "subdir")

	stats := stats.NewStats()
	_, err := NewCache(missingDir, real.NewSyscalls(stats))
	s.Error(err)

	_, err = os.Stat(missingDir)
	s.True(os.IsNotExist(err), "cache directory was created but should not have been")
}
