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

package test

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/stretchr/testify/suite"
)

// SuiteWithTempDir is an extension to suite.Suite that manages a temporary directory for all
// test cases to use.
//
// Suites implementing this type must be careful to invoke SetupTest() and TearDownTest() on their
// own if they redefine those methods.
type SuiteWithTempDir struct {
	suite.Suite

	tempDir string
}

var _ suite.SetupTestSuite = (*SuiteWithTempDir)(nil)
var _ suite.TearDownTestSuite = (*SuiteWithTempDir)(nil)

// SetupTest creates a temporary directory.  Fails the test case if the directory creation fails.
func (s *SuiteWithTempDir) SetupTest() {
	tempDir, err := ioutil.TempDir("", "test")
	s.Require().NoError(err)
	defer func() {
		// Only clean up the temporary directory if we haven't completed setup.
		if s.tempDir == "" {
			os.RemoveAll(tempDir)
		}
	}()

	// Now that setup went well, initialize all fields.  No operation that can fail should happen
	// after this point, or the cleanup routines scheduled with defer may do the wrong thing.
	s.tempDir = tempDir
}

// TearDownTest recursively destroys the temporary directory created for the test case.  Fails the
// test case if the destruction fails.
func (s *SuiteWithTempDir) TearDownTest() {
	s.NoError(os.RemoveAll(s.tempDir))
}

// TempDir retrieves the path to the temporary directory for the test case.
func (s *SuiteWithTempDir) TempDir() string {
	return s.tempDir
}

// CheckFileExists checks if a file exists and returns an error otherwise.
func CheckFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("expected file %s does not exist", path)
	}
	return nil
}

// CheckFileNotExists checks if a file does not exist and returns an error otherwise.
func CheckFileNotExists(path string) error {
	if _, err := os.Stat(path); os.IsExist(err) {
		return fmt.Errorf("unexpected file %s exists", path)
	}
	return nil
}

// CheckFileContents checks if a file matches the given contents.
func CheckFileContents(path string, expectedContents string) error {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	if string(contents) != expectedContents {
		return fmt.Errorf("file %s doesn't match expected contents: got '%s', want '%s'", path, contents, expectedContents)
	}
	return nil
}

// WriteFile creates or overwrites a file with the given contents.
func WriteFile(path string, contents string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	n, err := file.WriteString(contents)
	if err != nil {
		return err
	} else if n != len(contents) {
		return fmt.Errorf("failed to write contents in file %s: got %d length, want %d", path, n, len(contents))
	}
	return file.Close()
}
