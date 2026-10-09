#!/usr/bin/env bash
# GUARD FILE — docs/plans/panel-redesign.md § Enforcement, "design-first".
#
# Checks every commit of a push or pull request, one by one:
#
#   1. Design first. A commit that touches the accepted design — the mockups
#      under docs/assets/panel-redesign/panel/, the guard tests, this script —
#      touches nothing else under internal/web/. The guards are not adjusted
#      in the commit they are supposed to judge.
#   2. The ratchet. internal/web/view/legacy_pages.txt only shrinks: a commit's
#      list is a subset of its parent's. A page that was restyled stays held
#      to its mockup; its name cannot be put back.
#   3. No blob from the past. A template a commit adds or changes is not,
#      whitespace aside, a template that already existed anywhere in the
#      history before it. Taking an old page out of git is not a redesign.
#
# Usage: design-first.sh <base> <head>     (commits base..head are checked)
# Needs the full history: actions/checkout with fetch-depth: 0.
set -euo pipefail

base="${1:?usage: design-first.sh <base> <head>}"
head="${2:?usage: design-first.sh <base> <head>}"

RATCHET=internal/web/view/legacy_pages.txt
TEMPLATES=internal/web/view/templates/
ZERO=0000000000000000000000000000000000000000

# A new branch, or a force push whose old tip is gone, has no usable base:
# check the tip commit alone rather than nothing.
if [ "$base" = "$ZERO" ] || ! git cat-file -e "$base^{commit}" 2>/dev/null; then
  base="$(git rev-parse "$head^" 2>/dev/null || true)"
fi

if [ -n "$base" ]; then
  commits="$(git rev-list --reverse --no-merges "$base..$head")"
else
  commits="$(git rev-list --reverse --no-merges "$head")"
fi

is_guard() {
  case "$1" in
    docs/assets/panel-redesign/panel/*) return 0 ;;
    .github/scripts/design-first.sh) return 0 ;;
    internal/web/guard_*_test.go | internal/web/*/guard_*_test.go) return 0 ;;
  esac
  return 1
}

entries() { # the ratchet's entries at a commit, sorted; nothing if the file is absent
  git show "$1:$RATCHET" 2>/dev/null | tr -d '\r' | sed -e 's/#.*//' -e 's/[[:space:]]//g' -e '/^$/d' | sort -u
}

normalised() { # a blob's hash with all whitespace removed
  git cat-file blob "$1" | tr -d ' \t\r\n' | git hash-object --stdin
}

cache="$(mktemp -d)"
trap 'rm -rf "$cache"' EXIT

past_hash() { # normalised hash of a historical blob, computed once
  if [ ! -f "$cache/$1" ]; then
    normalised "$1" > "$cache/$1"
  fi
  cat "$cache/$1"
}

fail=0
say() {
  echo "::error::design-first: $*"
  fail=1
}

for c in $commits; do
  short="$(git rev-parse --short "$c")"
  parent="$(git rev-parse --verify --quiet "$c^" || true)"
  if [ -n "$parent" ]; then
    changed="$(git diff --name-only --no-renames "$parent" "$c")"
  else
    changed="$(git ls-tree -r --name-only "$c")"
  fi

  # 1. design first
  guards="" impl=""
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    if is_guard "$f"; then
      guards="$guards $f"
    elif [ "${f#internal/web/}" != "$f" ]; then
      impl="$impl $f"
    fi
  done <<< "$changed"
  if [ -n "$guards" ] && [ -n "$impl" ]; then
    say "$short changes the design or its guards ($(echo $guards | cut -c1-200)) together with the implementation ($(echo $impl | cut -c1-200)). A design change is its own 'design:' commit, made and accepted first."
  fi

  # 2. the ratchet
  if [ -n "$parent" ] && grep -qx "$RATCHET" <<< "$changed"; then
    # Once the file has existed, a parent without it means an empty list.
    if git cat-file -e "$parent:$RATCHET" 2>/dev/null || [ -n "$(git log -1 --format=%h "$parent" -- "$RATCHET")" ]; then
      added="$(comm -13 <(entries "$parent") <(entries "$c"))"
      if [ -n "$added" ]; then
        say "$short adds to $RATCHET: $(echo $added). The list only shrinks — what was held to the design stays held."
      fi
    fi
  fi

  # 3. no blob from the past
  if [ -n "$parent" ]; then
    touched="$(git diff --name-only --no-renames --diff-filter=AM "$parent" "$c" -- "$TEMPLATES")"
    if [ -n "$touched" ]; then
      past="$(git log --format= --raw --no-abbrev --no-renames "$parent" -- "$TEMPLATES" | awk '$4 != "'$ZERO'" {print $4}' | sort -u)"
      while IFS= read -r f; do
        [ -n "$f" ] || continue
        now="$(normalised "$(git rev-parse "$c:$f")")"
        for blob in $past; do
          if [ "$(past_hash "$blob")" = "$now" ]; then
            say "$short: $f is, whitespace aside, a template that already existed in history (blob ${blob:0:12}). An old template is not a redesigned page."
            break
          fi
        done
      done <<< "$touched"
    fi
  fi
done

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "design-first: $(echo "$commits" | grep -c . || true) commit(s) checked, nothing to report"
