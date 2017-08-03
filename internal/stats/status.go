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
	"html/template"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
)

const (
	statusHTML = `
<html>
<head>
<title>sourcachefs on {{ .StatsRoot }}</title>
<style type="text/css">
h1, h2 {
  background-color: #ddddff;
  padding: 5px;
}
table th, td {
  border: 1px solid;
}
</style>
</head>

<body>
<h1>sourcachefs on {{ .MountPoint }}</h1>

<p>Built on: {{ .BuildTimestamp }} by {{ .BuildWhere }}<br/>
Git revision: {{ .GitRevision }}</p>

<p><a href="/debug/pprof/">pprof</a></p>

<p><a href="{{ .StatsRoot }}/clear">Clear stats</a></p>

<h2>Fuse layer</h2>

<p>Operation counts:</p>

<table>
  <tr>
    <th>Operation</th>
    <th>Cache hits</th>
    <th>Cache misses</th>
    <th>Direct</th>
  </tr>

  {{ range .FuseOpStats }}
    <tr>
      <td>{{ .Name }}</td>
      <td>{{ .CacheHits }}</td>
      <td>{{ .CacheMisses }}</td>
      <td>{{ .Direct }}</td>
    </tr>
  {{ end }}
</table>

<h2>OS syscall layer</h2>

<p>Operation counts:</p>

<table>
  <tr>
    <th>Operation</th>
    <th>Local</th>
    <th>Remote</th>
  </tr>

  {{ range .OsOpStats }}
    <tr>
      <td>{{ .Name }}</td>
      <td>{{ .LocalCount }}</td>
      <td>{{ .RemoteCount }}</td>
    </tr>
  {{ end }}
</table>

<p>Data processed:</p>

<table>
  <tr>
    <th>Rx/Tx</th>
    <th>Local</th>
    <th>Remote</th>
  </tr>

  <tr>
    <td>Read</td>
    <td>{{ .LocalBytes }}</td>
    <td>{{ .RemoteBytes }}</td>
  </tr>
</table>

<p>Note: read counters do not account for file content served from the kernel
buffer cache.</p>

</body>
</html>
`
)

var (
	buildTimestamp string
	buildWhere     string
	gitRevision    string

	osOpNames = map[OsOp]string{
		CloseOsOp:    "Close",
		LstatOsOp:    "Lstat",
		MkdirOsOp:    "Mkdir",
		OpenOsOp:     "Open",
		ReadOsOp:     "Read",
		ReaddirOsOp:  "Readdir",
		ReadlinkOsOp: "Readlink",
		RemoveOsOp:   "Remove",
		RenameOsOp:   "Rename",
	}

	fuseOpNames = map[FuseOp]string{
		AttrFuseOp:     "Attr",
		LookupFuseOp:   "Lookup",
		OpenFuseOp:     "Open",
		ReadFuseOp:     "Read",
		ReaddirFuseOp:  "Readdir",
		ReadlinkFuseOp: "Readlink",
	}
)

type osOpStatsRow struct {
	Name        string
	LocalCount  uint64
	RemoteCount uint64
}

type osOpStatsSlice []osOpStatsRow

var _ (sort.Interface) = (*osOpStatsSlice)(nil)

func (slice osOpStatsSlice) Len() int {
	return len(slice)
}

func (slice osOpStatsSlice) Less(i, j int) bool {
	return strings.Compare(slice[i].Name, slice[j].Name) == -1
}

func (slice osOpStatsSlice) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

type fuseOpStatsRow struct {
	Name        string
	CacheHits   uint64
	CacheMisses uint64
	Direct      uint64
}

type fuseOpStatsSlice []fuseOpStatsRow

var _ (sort.Interface) = (*fuseOpStatsSlice)(nil)

func (slice fuseOpStatsSlice) Len() int {
	return len(slice)
}

func (slice fuseOpStatsSlice) Less(i, j int) bool {
	return strings.Compare(slice[i].Name, slice[j].Name) == -1
}

func (slice fuseOpStatsSlice) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

func generateStatusPage(root string, mountPoint string, stats Retriever, writer io.Writer) error {
	osOpStats := stats.GetOsOpStats()
	osOpRows := make([]osOpStatsRow, 0, len(osOpStats))
	for op, data := range osOpStats {
		osOpRows = append(osOpRows, osOpStatsRow{
			Name:        osOpNames[op],
			LocalCount:  data.LocalCount,
			RemoteCount: data.RemoteCount,
		})
	}

	readStats := stats.GetReadStats()

	fuseOpStats := stats.GetFuseOpStats()
	fuseOpRows := make([]fuseOpStatsRow, 0, len(fuseOpStats))
	for op, data := range fuseOpStats {
		fuseOpRows = append(fuseOpRows, fuseOpStatsRow{
			Name:        fuseOpNames[op],
			CacheHits:   data.CacheHits,
			CacheMisses: data.CacheMisses,
			Direct:      data.Direct,
		})
	}

	sort.Sort(osOpStatsSlice(osOpRows))
	sort.Sort(fuseOpStatsSlice(fuseOpRows))

	statusTemplate := template.Must(
		template.New("status").Parse(statusHTML))
	data := struct {
		BuildTimestamp string
		BuildWhere     string
		GitRevision    string
		StatsRoot      string
		MountPoint     string
		OsOpStats      osOpStatsSlice
		FuseOpStats    fuseOpStatsSlice
		LocalBytes     string
		RemoteBytes    string
	}{
		BuildTimestamp: buildTimestamp,
		BuildWhere:     buildWhere,
		GitRevision:    gitRevision,
		StatsRoot:      root,
		MountPoint:     mountPoint,
		OsOpStats:      osOpRows,
		FuseOpStats:    fuseOpRows,
		LocalBytes:     humanize.Bytes((uint64)(readStats[LocalDomain])),
		RemoteBytes:    humanize.Bytes((uint64)(readStats[RemoteDomain])),
	}

	return statusTemplate.Execute(writer, data)
}

// SetupHTTPHandlers configures the HTTP server with the endpoints to access statistics.
//
// The configured handlers operate on the given Stats object, and the endpoints are installed
// relative to the given root path.
func SetupHTTPHandlers(stats *Stats, root string, mountPoint string) {
	if len(buildTimestamp) == 0 {
		panic("Missing build timestamp")
	}
	if len(buildWhere) == 0 {
		panic("Missing build where")
	}
	if len(gitRevision) == 0 {
		panic("Missing git revision")
	}

	http.HandleFunc(root+"/summary", func(w http.ResponseWriter, r *http.Request) {
		err := generateStatusPage(root, mountPoint, stats, w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	http.HandleFunc(root+"/clear", func(w http.ResponseWriter, r *http.Request) {
		stats.Clear()
		http.Redirect(w, r, root+"/summary", http.StatusFound)
	})
}
