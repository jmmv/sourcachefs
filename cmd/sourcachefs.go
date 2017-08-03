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

package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof" // Automatically exports pprof endpoints.
	"os"
	"path/filepath"
	"regexp"

	"github.com/golang/glog"

	"github.com/jmmv/sourcachefs/internal/cache"
	"github.com/jmmv/sourcachefs/internal/fs"
	"github.com/jmmv/sourcachefs/internal/real"
	"github.com/jmmv/sourcachefs/internal/stats"
)

// progname computes and returns the name of the current program.
func progname() string {
	return filepath.Base(os.Args[0])
}

// flagUsage redirects the user to the -help flag.  This is the handler to pass to the flag module
// for usage errors.
func flagUsage() {
	fmt.Fprintf(os.Stderr, "Type '%s -help' for usage details\n", progname())
}

// usageError prints an error triggered by the user.
//
// This function receives an error instead of just a string so that the linter can catch malformed
// errors when formatted with fmt.Errorf.
func usageError(err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", progname(), err)
	flagUsage()
}

// runHTTPServer starts and runs the loop of the HTTP server.  We use this to serve stats and the
// pprof endpoints.
func runHTTPServer(address string, root string, s *stats.Stats) {
	glog.Infof("starting HTTP server listening on %s", address)
	stats.SetupHTTPHandlers(s, "/stats", root)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/stats/summary", http.StatusFound)
	})
	err := http.ListenAndServe(address, nil)
	if err != nil {
		glog.Fatalf("cannot start HTTP server: %s", err)
	}
}

// showHelp prints the interactive help usage message.
func showHelp() {
	fmt.Fprintf(os.Stderr, "Usage: %s <target_dir> <cache_dir> <mount_point>\n\n", progname())
	flag.PrintDefaults()
}

// main is the program entry point.
func main() {
	// TODO(jmmv): Add daemon mode and enable as default.
	var help = flag.Bool("help", false, "show help and exit")
	var listenAddress = flag.String("listen_address", "", "enable HTTP server on the given address for pprof support and statistics tracking")
	var cachedPathRegex = flag.String("cached_path_regex", ".*", "only cache files whose path relative to the root match this regex")
	flag.Usage = flagUsage
	flag.Parse()

	if *help {
		if flag.NArg() != 0 {
			usageError(fmt.Errorf("invalid number of arguments; expected 0"))
			os.Exit(2)
		}
		showHelp()
		os.Exit(0)
	}

	if flag.NArg() != 3 {
		usageError(fmt.Errorf("invalid number of arguments; expected 3"))
		os.Exit(2)
	}
	targetDir := flag.Arg(0)
	cacheDir := flag.Arg(1)
	mountPoint := flag.Arg(2)

	compiledCachedPathRegex, err := regexp.Compile(*cachedPathRegex)
	if err != nil {
		glog.Fatalf("failed to compile regexp %s: %s", *cachedPathRegex, err)
	}

	stats := stats.NewStats()
	syscalls := real.NewSyscalls(stats)

	cache, err := cache.NewCache(cacheDir, syscalls)
	if err != nil {
		glog.Fatalf("failed to initialize cache: %s", err)
	}
	defer cache.Close()

	if len(*listenAddress) > 0 {
		go runHTTPServer(*listenAddress, mountPoint, stats)
	}

	err = fs.Loop(targetDir, syscalls, cache, stats, mountPoint, compiledCachedPathRegex)
	if err != nil {
		glog.Fatalf("failed to serve file system: %s", err)
	}
}
