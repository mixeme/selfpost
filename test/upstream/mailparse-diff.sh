#!/bin/sh
# The From: parser port (internal/milter/mailparse.go) against OpenDKIM's own
# dkim_mail_parse(): builds internal/milter/testdata/mailparse_harness.c
# against the libopendkim source the image runs, in a debian:trixie
# container, and runs TestMailParseMatchesOpenDKIM with it.
#
#   test/upstream/mailparse-diff.sh <opendkim source dir>
#
# The source dir is what `apt-get source opendkim` unpacks in debian:trixie
# (docs/development.md); it must contain libopendkim/.
set -eu
src=$(cd "$1" && pwd)
repo=$(cd "$(dirname "$0")/../.." && pwd)
docker run --rm -v "$src":/src:ro -v "$repo/internal/milter/testdata":/h -w /h debian:trixie sh -c '
apt-get update -qq >/dev/null && apt-get install -y -qq gcc libc6-dev >/dev/null 2>&1
gcc -O1 -static -o /h/mailparse_harness -I /src/libopendkim /h/mailparse_harness.c'
trap 'rm -f "$repo/internal/milter/testdata/mailparse_harness"' EXIT
cd "$repo" && MAILPARSE_C="$repo/internal/milter/testdata/mailparse_harness" \
	go test -count=1 -v -run 'TestMailParseMatchesOpenDKIM' ./internal/milter
