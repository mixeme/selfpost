#!/bin/sh
# base-packages.sh: compare the Debian packages the mail path runs on with
# what debian:trixie-slim would install today (security audit 2026-10-17,
# I5). The image installs without pinning, and published tags are never
# rebuilt, so a security update of postfix, Cyrus SASL, OpenSSL or Berkeley
# DB reaches operators only with the next release. This fails when one of
# them has moved past the version the last release was built with - the
# table between the base-package-versions markers in docs/development.md -
# as the nudge to cut a patch release (and then to update the table).
#
# Run by .github/workflows/base-packages.yml every week; needs docker.
set -eu
doc=docs/development.md
table=$(sed -n '/<!-- base-package-versions -->/,/<!-- \/base-package-versions -->/p' "$doc" |
	sed -n 's/^| `\([^`]*\)` | `\([^`]*\)` |$/\1 \2/p')
[ -n "$table" ] || { echo "no base-package-versions table in $doc" >&2; exit 2; }
names=$(echo "$table" | cut -d' ' -f1 | tr '\n' ' ')
# apt-cache policy's "Candidate:" is what an install would take now.
now=$(docker run --rm debian:trixie-slim sh -c "apt-get update -qq >/dev/null && for p in $names; do printf '%s %s\n' \"\$p\" \"\$(apt-cache policy \"\$p\" | sed -n 's/^ *Candidate: //p')\"; done")
moved=0
echo "$table" | while read -r name built; do
	cand=$(echo "$now" | awk -v n="$name" '$1 == n { print $2 }')
	if [ "$cand" != "$built" ]; then
		echo "::warning::$name: the last release was built with $built, trixie now installs ${cand:-nothing}" >&2
		echo moved
	fi
done | grep -q moved && moved=1
if [ "$moved" = 1 ]; then
	echo "A package the mail path runs on has moved: cut a patch release, then update the table in $doc." >&2
	exit 1
fi
echo "Every watched package is as the last release was built."
