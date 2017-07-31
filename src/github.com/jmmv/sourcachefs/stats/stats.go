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

package stats

import (
	"sync/atomic"
)

// OsOp represents a kernel-level file operation initiated by this file system.
type OsOp int

// Kernel-level file operations initiated by this file system.
const (
	CloseOsOp    OsOp = iota
	LstatOsOp    OsOp = iota
	MkdirOsOp    OsOp = iota
	OpenOsOp     OsOp = iota
	ReadOsOp     OsOp = iota
	ReaddirOsOp  OsOp = iota
	ReadlinkOsOp OsOp = iota
	RemoveOsOp   OsOp = iota
	RenameOsOp   OsOp = iota
)

// allOsOps contains the list of all possible kernel-level system calls tracked in statistics.
var allOsOps = []OsOp{CloseOsOp, LstatOsOp, MkdirOsOp, OpenOsOp, ReadOsOp, ReaddirOsOp, ReadlinkOsOp, RemoveOsOp, RenameOsOp}

// OpDomain indicates where the file operation happened.
type OpDomain int

// Identifier for the source of a file operation.
const (
	// LocalDomain are operations issued against the local file system.
	LocalDomain OpDomain = iota

	// RemoteDomain are operations issued against the target of the sourcachefs file system.
	RemoteDomain OpDomain = iota
)

// allDomains contains the list of all possible operation domains tracked in statistics.
var allDomains = []OpDomain{LocalDomain, RemoteDomain}

// PerOsOpStats contains the statistics for a specific kernel-level system call.
type PerOsOpStats struct {
	LocalCount  uint64
	RemoteCount uint64
}

// FuseOp represents a FUSE-level file operation received by this file system.
type FuseOp int

// FUSE-level file operations received by this file system.
const (
	AttrFuseOp     FuseOp = iota
	LookupFuseOp   FuseOp = iota
	OpenFuseOp     FuseOp = iota
	ReadFuseOp     FuseOp = iota
	ReaddirFuseOp  FuseOp = iota
	ReadlinkFuseOp FuseOp = iota
)

// allFuseOps contains the list of all possible FUSE-level operations tracked in statistics.
var allFuseOps = []FuseOp{AttrFuseOp, LookupFuseOp, OpenFuseOp, ReadFuseOp, ReaddirFuseOp, ReadlinkFuseOp}

// PerFuseOpStats contains the statistics for a specific FUSE-level served call.
type PerFuseOpStats struct {
	CacheHits   uint64
	CacheMisses uint64
	Direct      uint64
}

// OsStatsUpdater is the interface that groups the operations to update statistics about the
// kernel-level operations initiated by this file system.
type OsStatsUpdater interface {
	AccountOsOp(OpDomain, OsOp)
	AccountReadBytes(OpDomain, int)
}

// FuseStatsUpdater is the interface that groups the operations to update statistics about the
// FUSE-level operations received by this file system.
type FuseStatsUpdater interface {
	AccountCacheHit(FuseOp)
	AccountCacheMiss(FuseOp)
	AccountDirect(FuseOp)
}

// Clearer is the interface that groups the operations to reset the statistics.
type Clearer interface {
	Clear()
}

// Updater is the interface that groups the operations to modify statistics in any way.
type Updater interface {
	OsStatsUpdater
	FuseStatsUpdater
	Clearer
}

// Retriever is the interface that groups the operations to fetch statistics.
type Retriever interface {
	GetOsOpStats() map[OsOp]PerOsOpStats
	GetReadStats() map[OpDomain]Bytes
	GetFuseOpStats() map[FuseOp]PerFuseOpStats
}

// Bytes represents a bytes quantity.
type Bytes uint64

// Stats contains the statistics data for the file system.
type Stats struct {
	osOpStats   map[OsOp]*PerOsOpStats
	readStats   map[OpDomain]*Bytes
	fuseOpStats map[FuseOp]*PerFuseOpStats
}

var _ FuseStatsUpdater = (*Stats)(nil)
var _ OsStatsUpdater = (*Stats)(nil)
var _ Clearer = (*Stats)(nil)
var _ Retriever = (*Stats)(nil)
var _ Updater = (*Stats)(nil)

// NewStats creates a new Stats instance with all stats set to their nil value.
func NewStats() *Stats {
	osOpStats := make(map[OsOp]*PerOsOpStats)
	for _, op := range allOsOps {
		osOpStats[op] = &PerOsOpStats{}
	}

	readStats := make(map[OpDomain]*Bytes)
	for _, domain := range allDomains {
		readStats[domain] = new(Bytes)
	}

	fuseOpStats := make(map[FuseOp]*PerFuseOpStats)
	for _, op := range allFuseOps {
		fuseOpStats[op] = &PerFuseOpStats{}
	}

	return &Stats{
		osOpStats:   osOpStats,
		readStats:   readStats,
		fuseOpStats: fuseOpStats,
	}
}

// Clear resets all statistics to their nil value.
func (stats *Stats) Clear() {
	for _, op := range allOsOps {
		opStats := stats.osOpStats[op]
		atomic.StoreUint64(&opStats.LocalCount, 0)
		atomic.StoreUint64(&opStats.RemoteCount, 0)
	}

	for _, domain := range allDomains {
		atomic.StoreUint64((*uint64)(stats.readStats[domain]), 0)
	}

	for _, op := range allFuseOps {
		opStats := stats.fuseOpStats[op]
		atomic.StoreUint64(&opStats.CacheHits, 0)
		atomic.StoreUint64(&opStats.CacheMisses, 0)
		atomic.StoreUint64(&opStats.Direct, 0)
	}
}

func (stats *Stats) getPerOsOpStats(op OsOp) *PerOsOpStats {
	opStats, ok := stats.osOpStats[op]
	if !ok {
		panic("OS operation not previously registered in map")
	}
	return opStats
}

func (stats *Stats) getPerFuseOpStats(op FuseOp) *PerFuseOpStats {
	opStats, ok := stats.fuseOpStats[op]
	if !ok {
		panic("Fuse operation not previously registered in map")
	}
	return opStats
}

// AccountOsOp increases the counter for the given OsOp by 1 on the given OpDomain.
func (stats *Stats) AccountOsOp(domain OpDomain, op OsOp) {
	opStats := stats.getPerOsOpStats(op)
	switch domain {
	case LocalDomain:
		atomic.AddUint64(&opStats.LocalCount, 1)
	case RemoteDomain:
		atomic.AddUint64(&opStats.RemoteCount, 1)
	default:
		panic("Unknown domain")
	}
}

// AccountReadBytes increases the read bytes quantity on the given OpDomain by the given amount.
func (stats *Stats) AccountReadBytes(domain OpDomain, bytes int) {
	count, ok := stats.readStats[domain]
	if !ok {
		panic("Unknown domain")
	}
	atomic.AddUint64((*uint64)(count), uint64(bytes))
}

// AccountCacheHit increases the cache hit counter for the given FuseOp by 1.
//
// Operations that count towards cache hits are those FUSE operations received by sourcachefs that
// did not have to spill into the remote file system.  These do not account for direct operations.
func (stats *Stats) AccountCacheHit(op FuseOp) {
	opStats := stats.getPerFuseOpStats(op)
	atomic.AddUint64(&opStats.CacheHits, 1)
}

// AccountCacheMiss increases the cache miss counter for the given FuseOp by 1.
//
// Operations that count towards cache misses are those FUSE operations received by sourcachefs
// that incurred a request into the remote file system.  These do not account for direct operations.
func (stats *Stats) AccountCacheMiss(op FuseOp) {
	opStats := stats.getPerFuseOpStats(op)
	atomic.AddUint64(&opStats.CacheMisses, 1)
}

// AccountDirect increases the direct counter for the given FuseOp by 1.
//
// Operations that count towards direct are those FUSE operations received by sourcachefs that
// were served locally (because the file system was explicitly configured as such).
func (stats *Stats) AccountDirect(op FuseOp) {
	opStats := stats.getPerFuseOpStats(op)
	atomic.AddUint64(&opStats.Direct, 1)
}

// GetOsOpStats retrieves details about all kernel-level operations initiated by this file system.
func (stats *Stats) GetOsOpStats() map[OsOp]PerOsOpStats {
	opStats := make(map[OsOp]PerOsOpStats)
	for op, data := range stats.osOpStats {
		opStats[op] = PerOsOpStats{
			LocalCount:  atomic.LoadUint64(&data.LocalCount),
			RemoteCount: atomic.LoadUint64(&data.RemoteCount),
		}
	}
	return opStats
}

// GetReadStats retrieves the total amount of data read, broken down by domain.
func (stats *Stats) GetReadStats() map[OpDomain]Bytes {
	readStats := make(map[OpDomain]Bytes)
	for domain, count := range stats.readStats {
		readStats[domain] = Bytes(atomic.LoadUint64((*uint64)(count)))
	}
	return readStats
}

// GetFuseOpStats retrieves details about all FUSE-level operations recieved by this file system.
func (stats *Stats) GetFuseOpStats() map[FuseOp]PerFuseOpStats {
	opStats := make(map[FuseOp]PerFuseOpStats)
	for op, data := range stats.fuseOpStats {
		opStats[op] = PerFuseOpStats{
			CacheHits:   atomic.LoadUint64(&data.CacheHits),
			CacheMisses: atomic.LoadUint64(&data.CacheMisses),
			Direct:      atomic.LoadUint64(&data.Direct),
		}
	}
	return opStats
}
