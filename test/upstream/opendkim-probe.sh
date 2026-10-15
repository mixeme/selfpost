#!/bin/sh
# The From: parser port against OpenDKIM's signing: starts OpenDKIM from the
# SelfPost image (default selfpost:e2e, the one make e2e builds) in a
# throwaway container with keys for three names, and runs
# TestPortAgainstOpenDKIM there against its socket.
#
#   test/upstream/opendkim-probe.sh [image]
set -eu
image=${1:-selfpost:e2e}
repo=$(cd "$(dirname "$0")/../.." && pwd)
bin=$(mktemp)
trap 'rm -f "$bin"' EXIT
cd "$repo" && CGO_ENABLED=0 GOOS=linux go test -c -o "$bin" ./internal/milter
docker run --rm -v "$repo/internal/milter":/pkg:ro -v "$bin":/milter.test:ro --entrypoint sh "$image" -c '
set -e
mkdir -p /tmp/odk && cd /tmp/odk
for d in sender.e2e.test other.e2e.test mail.e2e.test; do
	mkdir -p keys/$d
	opendkim-genkey -D keys/$d -d $d -s sel
	echo "$d $d:sel:/tmp/odk/keys/$d/sel.private" >> KeyTable
	echo "*@$d $d" >> SigningTable
done
printf "Mode s\nKeyTable /tmp/odk/KeyTable\nSigningTable refile:/tmp/odk/SigningTable\nSocket local:/tmp/odk/sock\nSyslog no\nRequireSafeKeys no\nBackground no\n" > o.conf
opendkim -V | head -1
opendkim -f -x /tmp/odk/o.conf &
for i in 1 2 3 4 5 6 7 8 9 10; do [ -S /tmp/odk/sock ] && break; sleep 0.5; done
cd /pkg
OPENDKIM_PROBE_SOCKET=/tmp/odk/sock OPENDKIM_PROBE_TABLE=/tmp/odk/SigningTable /milter.test -test.run TestPortAgainstOpenDKIM -test.v -test.timeout 600s'
