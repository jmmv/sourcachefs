#!/bin/sh
# Copyright 2017 Google Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not
# use this file except in compliance with the License.  You may obtain a copy
# of the License at:
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  See the
# License for the specific language governing permissions and limitations
# under the License.

set -e -u

# Default to no features to avoid cluttering .travis.yml.
: "${FEATURES:=}"

install_fuse() {
  case "${TRAVIS_OS_NAME}" in
    linux)
      sudo apt-get update
      sudo apt-get install -qq fuse libfuse-dev pkg-config

      sudo /bin/sh -c 'echo user_allow_other >>/etc/fuse.conf'
      sudo chmod 644 /etc/fuse.conf
      ;;

    osx)
      brew update
      brew cask install osxfuse

      sudo /Library/Filesystems/osxfuse.fs/Contents/Resources/load_osxfuse
      sudo sysctl -w vfs.generic.osxfuse.tunables.allow_other=1
      ;;

    *)
      echo "Don't know how to install FUSE for OS ${TRAVIS_OS_NAME}" 1>&2
      exit 1
      ;;
  esac
}

install_fuse
