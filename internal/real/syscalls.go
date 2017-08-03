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

package real

import (
	"fmt"
	"io"
	"os"

	"github.com/golang/glog"

	"github.com/jmmv/sourcachefs/internal/stats"
)

// FileReader is the interface to invoke read-only operations with statistics tracking.
type FileReader interface {
	io.Closer
	io.Reader
	io.ReaderAt
	Readdir(int) ([]os.FileInfo, error)
}

type tracedReader struct {
	stats  stats.OsStatsUpdater
	domain stats.OpDomain
	file   *os.File
}

var _ io.Closer = (*tracedReader)(nil)

func (tr *tracedReader) Close() error {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Close for %s", tr.file.Name()))
	tr.stats.AccountOsOp(tr.domain, stats.CloseOsOp)
	return tr.file.Close()
}

var _ io.Reader = (*tracedReader)(nil)

func (tr *tracedReader) Read(p []byte) (int, error) {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Read for %s", tr.file.Name()))
	tr.stats.AccountOsOp(tr.domain, stats.ReadOsOp)
	n, err := tr.file.Read(p)
	if err == nil && n > 0 {
		tr.stats.AccountReadBytes(tr.domain, n)
	}
	return n, err
}

var _ io.ReaderAt = (*tracedReader)(nil)

func (tr *tracedReader) ReadAt(p []byte, off int64) (int, error) {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Read for %s", tr.file.Name()))
	tr.stats.AccountOsOp(tr.domain, stats.ReadOsOp)
	n, err := tr.file.ReadAt(p, off)
	if err == nil && n > 0 {
		tr.stats.AccountReadBytes(tr.domain, n)
	}
	return n, err
}

func (tr *tracedReader) Readdir(n int) ([]os.FileInfo, error) {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Readdir for %s", tr.file.Name()))
	tr.stats.AccountOsOp(tr.domain, stats.ReaddirOsOp)
	return tr.file.Readdir(n)
}

// Syscalls is the interface to invoke kernel-level operations with statistics tracking.
type Syscalls interface {
	Lstat(stats.OpDomain, string) (os.FileInfo, error)
	Mkdir(stats.OpDomain, string, os.FileMode) error
	Open(stats.OpDomain, string) (FileReader, error)
	Readlink(stats.OpDomain, string) (string, error)
	Remove(stats.OpDomain, string) error
	Rename(stats.OpDomain, string, string) error
}

type syscalls struct {
	stats stats.OsStatsUpdater
}

// NewSyscalls creates a new Syscalls object that accounts statistics towards s.
func NewSyscalls(s stats.OsStatsUpdater) Syscalls {
	return &syscalls{
		stats: s,
	}
}

var _ Syscalls = (*syscalls)(nil)

func (sc *syscalls) Lstat(domain stats.OpDomain, path string) (os.FileInfo, error) {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Lstat for %s", path))
	sc.stats.AccountOsOp(domain, stats.LstatOsOp)
	return os.Lstat(path)
}

func (sc *syscalls) Mkdir(domain stats.OpDomain, path string, mode os.FileMode) error {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Mkdir for %s", path))
	sc.stats.AccountOsOp(domain, stats.MkdirOsOp)
	return os.Mkdir(path, mode)
}

func (sc *syscalls) Open(domain stats.OpDomain, path string) (FileReader, error) {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Open for %s", path))
	sc.stats.AccountOsOp(domain, stats.OpenOsOp)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &tracedReader{
		stats:  sc.stats,
		domain: domain,
		file:   file,
	}, nil
}

func (sc *syscalls) Readlink(domain stats.OpDomain, path string) (string, error) {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Readlink for %s", path))
	sc.stats.AccountOsOp(domain, stats.ReadlinkOsOp)
	return os.Readlink(path)
}

func (sc *syscalls) Rename(domain stats.OpDomain, from string, to string) error {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Rename for %s to %s", from, to))
	sc.stats.AccountOsOp(domain, stats.RenameOsOp)
	return os.Rename(from, to)
}

func (sc *syscalls) Remove(domain stats.OpDomain, path string) error {
	glog.InfoDepth(1, fmt.Sprintf("issuing syscall Remove for %s", path))
	sc.stats.AccountOsOp(domain, stats.RemoveOsOp)
	return os.Remove(path)
}
