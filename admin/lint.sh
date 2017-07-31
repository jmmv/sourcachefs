#! /bin/sh
# Copyright 2016 Google Inc.
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

ProgName="${0##*/}"

: ${GOFMT:=gofmt}
: ${GOLINT:=golint}

warn() {
    echo "${ProgName}: W: ${@}" 1>&2
}

lint_header() {
    local file="${1}"; shift

    local failed=no
    if ! grep 'Copyright.*Google' "${file}" >/dev/null; then
        warn "${file} does not have a copyright heading"
        failed=yes
    fi
    if ! grep 'Apache License.*2.0' "${file}" >/dev/null; then
        warn "${file} does not have a license notice"
        failed=yes
    fi
    [ "${failed}" = no ]
}

lint_gofmt() {
    local gofile="${1}"; shift
    local tmpdir="${1}"; shift

    "${GOFMT}" -e -s -d "${gofile}" >"${tmpdir}/gofmt.out" 2>&1
    if [ ${?} -ne 0 -o -s "${tmpdir}/gofmt.out" ]; then
        warn "${gofile} failed gofmt validation:"
        cat "${tmpfile}/gofmt.out" 1>&2
        return 1
    fi
    return 0
}

lint_golint() {
    local gofile="${1}"; shift
    local tmpdir="${1}"; shift

    # Lower confidence levels raise a per-file warning to remind about having
    # a package-level docstring... but the warning is issued blindly, without
    # checking for the existing of this docstring in other packages.
    local min_confidence=0.3

    "${GOLINT}" -min_confidence="${min_confidence}" "${gofile}" \
        >"${tmpdir}/golint.out"
    if [ ${?} -ne 0 -o -s "${tmpdir}/golint.out" ]; then
        warn "${gofile} failed golint validation:"
        cat "${tmpdir}/golint.out" 1>&2
        return 1
    fi
    return 0
}

main() {
    # Lint all source files by default if none are provided.
    [ ${#} -gt 1 ] || set -- $(find . -name .git -o -name deps -prune \
        -o \( -name "*.go" -o -name Makefile.in -o -name "*.sh" \) -print)

    local ok=yes

    local tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/lint.XXXXXX")"
    trap "rm -rf '${tmpdir}'" EXIT

    for file in "${@}"; do
        case "${file}" in
            AUTHORS|CONTRIBUTING|CONTRIBUTORS|LICENSE|README.md)
                continue
                ;;
        esac

        lint_header "${file}" || ok=no

        case "${file}" in
            *.go)
                lint_gofmt "${file}" "${tmpdir}" || ok=no
                lint_golint "${file}" "${tmpdir}" || ok=no
            ;;
        esac
    done

    [ "${ok}" = yes ]
}

main "${@}"
