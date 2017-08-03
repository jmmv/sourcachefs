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

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"bazil.org/fuse"

	"github.com/jmmv/sourcachefs/internal/test"
	"github.com/stretchr/testify/suite"
)

var (
	sourcachefs = mustGetenv("SOURCACHEFS")
)

// mustGetenv gets an environment variable and panics when not defined.  To be used from a static
// context.
func mustGetenv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		panic(name + " not defined in environment")
	}
	return value
}

// runData holds runtime information for a sourcachefs execution.
type runData struct {
	cmd *exec.Cmd
	out bytes.Buffer
	err bytes.Buffer
}

// run starts a background process to run sourcachefs and passes it the given arguments.
func run(s *suite.Suite, arg ...string) *runData {
	var data runData
	data.cmd = exec.Command(sourcachefs, arg...)
	data.cmd.Stdout = &data.out
	data.cmd.Stderr = &data.err
	s.NoError(data.cmd.Start())
	return &data
}

// wait awaits for completion of the process started by run and checks its exit status.
func wait(s *suite.Suite, data *runData, expectedExitStatus int) {
	err := data.cmd.Wait()
	if expectedExitStatus == 0 {
		s.NoError(err)
	} else {
		status := err.(*exec.ExitError).ProcessState.Sys().(syscall.WaitStatus)
		s.Equal(expectedExitStatus, status.ExitStatus())
	}
}

type FunctionalSuite struct {
	test.SuiteWithTempDir

	targetDir  string
	cacheDir   string
	mountPoint string

	cmd *exec.Cmd
}

func TestFunctional(t *testing.T) {
	suite.Run(t, new(FunctionalSuite))
}

func (s *FunctionalSuite) SetupTest() {
	s.SuiteWithTempDir.SetupTest()

	targetDir := filepath.Join(s.TempDir(), "target")
	cacheDir := filepath.Join(s.TempDir(), "cache")
	mountPoint := filepath.Join(s.TempDir(), "mnt")
	for _, dir := range []string{targetDir, cacheDir, mountPoint} {
		s.Require().NoError(os.Mkdir(dir, 0755))
	}

	s.Require().NoError(test.WriteFile(filepath.Join(targetDir, "cookie"), ""))

	cmd := exec.Command(sourcachefs, targetDir, cacheDir, mountPoint)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	s.Require().NoError(cmd.Start())
	defer func() {
		// Only clean up the child process if we haven't completed cleanup.
		if s.cmd == nil {
			cmd.Process.Kill()
			s.Error(cmd.Wait())
			fuse.Unmount(mountPoint) // Best effort; errors don't matter.
		}
	}()

	for tries := 0; tries < 10; tries++ {
		_, err := os.Stat(filepath.Join(mountPoint, "cookie"))
		if err == nil {
			break
		}
		if err != nil && tries == 9 {
			s.Require().NoErrorf(err, "File system failed to come up")
		}
		time.Sleep(100 * 1000 * 1000) // Duration is in nanoseconds.
	}

	// Now that setup went well, initialize all fields.  No operation that can fail should happen
	// after this point, or the cleanup routines scheduled with defer may do the wrong thing.
	s.targetDir = targetDir
	s.cacheDir = cacheDir
	s.mountPoint = mountPoint
	s.cmd = cmd
}

func (s *FunctionalSuite) TearDownTest() {
	if s.cmd != nil {
		s.NoError(fuse.Unmount(s.mountPoint)) // Causes server to exit cleanly.
		s.NoError(s.cmd.Wait())
	}

	s.SuiteWithTempDir.TearDownTest()
}

func (s *FunctionalSuite) TestBasicCaching() {
	// Create a file and check that it appears in the mount point.
	s.NoError(test.WriteFile(filepath.Join(s.targetDir, "a"), "foo"))
	s.NoError(test.CheckFileContents(filepath.Join(s.mountPoint, "a"), "foo"))

	// Overwrite the previous file and check that we still see the old contents in the mount point.
	s.NoError(test.WriteFile(filepath.Join(s.targetDir, "a"), "bar"))
	s.NoError(test.CheckFileContents(filepath.Join(s.mountPoint, "a"), "foo"))

	// Delete the previous file and check that we still see the old contents in the mount point.
	s.NoError(os.Remove(filepath.Join(s.targetDir, "a")))
	s.NoError(test.CheckFileExists(filepath.Join(s.mountPoint, "a")))

	// Check that a non-existent file also does not appear in the mount point.
	s.NoError(test.CheckFileNotExists(filepath.Join(s.mountPoint, "b")))

	// Create the checked file and check that it still does not appear in the mount point.
	s.NoError(test.WriteFile(filepath.Join(s.targetDir, "b"), "baz"))
	s.NoError(test.CheckFileNotExists(filepath.Join(s.mountPoint, "b")))
}

func (s *FunctionalSuite) TestSignalHandling() {
	s.NoError(test.WriteFile(filepath.Join(s.targetDir, "a"), ""))
	s.NoError(test.CheckFileExists(filepath.Join(s.mountPoint, "a")))

	s.NoError(s.cmd.Process.Signal(os.Interrupt))
	s.Error(s.cmd.Wait())
	s.False(s.cmd.ProcessState.Success())

	s.Error(fuse.Unmount(s.mountPoint))
	s.NoError(test.CheckFileNotExists(filepath.Join(s.mountPoint, "a")))

	s.cmd = nil // Tell tearDown that we did the cleanup ourselves.
}

type FlagParsingSuite struct {
	suite.Suite
}

func TestFlagParsing(t *testing.T) {
	suite.Run(t, new(FlagParsingSuite))
}

func (s *FlagParsingSuite) TestInvalidSyntax() {
	data := []struct {
		args           []string
		expectedStderr string
	}{
		{[]string{"--foo"}, "not defined.*-foo"},
		{[]string{"foo"}, "number of arguments.* expected 3"},
		{[]string{"foo", "bar"}, "number of arguments.* expected 3"},
		{[]string{"foo", "bar", "ok", "baz"}, "number of arguments.* expected 3"},
	}
	for _, d := range data {
		cmd := run(&s.Suite, d.args...)
		wait(&s.Suite, cmd, 2)
		s.Empty(cmd.out.String())
		s.Regexp(d.expectedStderr, cmd.err.String(), "No error details found")
		s.Regexp("Type 'sourcachefs -help'", cmd.err.String(), "No instructions found")
	}

	cmd := run(&s.Suite, "--foo")
	wait(&s.Suite, cmd, 2)
	s.Empty(cmd.out.String())
	s.Regexp("not defined.*-foo", cmd.err.String(), "No error details found")
	s.Regexp("Type 'sourcachefs -help'", cmd.err.String(), "No instructions found")
}

func (s *FlagParsingSuite) TestHelpOk() {
	data := []struct {
		args []string
	}{
		{[]string{"--help"}},
		{[]string{"--stderrthreshold", "error", "--help"}},
		{[]string{"--help", "--stderrthreshold", "error"}},
	}
	for _, d := range data {
		cmd := run(&s.Suite, d.args...)
		wait(&s.Suite, cmd, 0)
		s.Empty(cmd.out.String())
		s.Regexp("Usage: sourcachefs .*mount_point", cmd.err.String(), "No usage line found")
		s.Regexp("-logtostderr", cmd.err.String(), "No flag information found")
	}
}

func (s *FlagParsingSuite) TestHelpWithInvalidSyntax() {
	data := []struct {
		args           []string
		expectedStderr string
	}{
		{[]string{"--invalid_flag", "--help"}, "not defined.*-invalid_flag"},
		{[]string{"--help", "foo"}, "number of arguments.* expected 0"},
	}
	for _, d := range data {
		cmd := run(&s.Suite, d.args...)
		wait(&s.Suite, cmd, 2)
		s.Empty(cmd.out.String())
		s.Regexp(d.expectedStderr, cmd.err.String(), "No error details found")
		s.Regexp("Type 'sourcachefs -help'", cmd.err.String(), "No instructions found")
	}
}
