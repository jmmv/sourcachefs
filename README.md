# sourcachefs

sourcachefs is a **persistent, read-only, FUSE-based caching file system**.
The goal of sourcachefs is to provide a caching layer over immutable slow
file systems, possibly networked ones such as SSHFS.

sourcachefs was initially designed to cache source files exposed via a
slow networked file system, where this file system maintains immutable views
of each commit from a version control system.  In this scheme, each commit is
a separate directory and can be cached indefinitely because the contents
are assumed to not change.

sourcachefs should support offline operation: once the contents of the
remote file system have been cached, operations on those same files and
directories should continue to work locally.

This is not an official Google product.

## Installation

Run:

    ./configure && make

This command will download all required Go dependencies into a temporary
directory, apply necessary patches to them, and build sourcachefs.  (This is
a bit of a non-standard Go workflow for your sanity, but if you don't care
about fetching dependencies correctly, you can still use `go get` and
`go build` as usual.)

The results of the build will be left as the self-contained binary
`bin/sourcachefs`.  You can copy it anywhere you want it to live.

## Usage

The basic usage is:

    sourcachefs <target_dir> <cache_dir> <mount_point>

where `target_dir` is the (slow) file system to be reexposed at `mount_point`
and `cache_dir` is the directory that will hold the persistent cached
contents.

The following non-standard flags are recognized:

* `--cached_path_regex=string`: Specifies a regular expression to match
  relative paths within the mount point.  If there is a match, the file is
  cached by sourcachefs; if there is not, accesses go directly to the
  target directory.

* `--listen_address=string`: Enables an HTTP server on the given address
  for pprof support and for statistics tracking.  Statistics are useful to
  see how much the cache is helping, if at all, to avoid falling through
  the remote file system.

## Contributing

Want to contribute?  Great!  But please first read the guidelines provided
in [CONTRIBUTING](CONTRIBUTING).

If you are curious about who made this project possible, you can check out
the [list of copyright holders](AUTHORS) and the [list of
individuals](CONTRIBUTORS).
