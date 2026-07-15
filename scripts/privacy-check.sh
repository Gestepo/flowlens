#!/bin/sh
set -eu

umask 077
requested_root=${FLOWLENS_PRIVACY_ROOT:-.}
root=$(git -C "$requested_root" rev-parse --show-toplevel 2>/dev/null) || {
  echo "privacy check requires a Git worktree" >&2
  exit 2
}
root=$(CDPATH='' cd -- "$root" && pwd)

tracked=$(mktemp)
patterns=$(mktemp)
trap 'rm -f "$tracked" "$patterns"' EXIT HUP INT TERM
git -C "$root" ls-files -z >"$tracked"

fail() {
  echo "privacy check failed: $1" >&2
  exit 1
}

status=0
grep -z -q -E '^docs/superpowers(/|$)' "$tracked" || status=$?
case "$status" in
  0) fail "internal planning material is tracked" ;;
  1) ;;
  *) echo "privacy check could not scan staged paths" >&2; exit 2 ;;
esac

for expression in \
  '/r[o]ot/' \
  '/h[o]me/[^/[:space:]]+/' \
  'flowlens-(server|agent)-[0-9]+' \
  'flowlens-(server|agent):[^[:space:]]+' \
  'docker[[:space:]]+compose'
do
  status=0
  grep -z -q -E "$expression" "$tracked" || status=$?
  case "$status" in
    0) fail "a staged path violates a structural privacy rule" ;;
    1) ;;
    *) echo "privacy check could not scan staged paths" >&2; exit 2 ;;
  esac

  status=0
  git -C "$root" grep --cached -q -E "$expression" -- || status=$?
  case "$status" in
    0) fail "staged content violates a structural privacy rule" ;;
    1) ;;
    *) echo "privacy check could not scan staged content" >&2; exit 2 ;;
  esac
done

denylist=${FLOWLENS_PRIVACY_DENYLIST_FILE:-}
if [ -n "$denylist" ]; then
  if [ ! -f "$denylist" ] || [ -L "$denylist" ]; then
    echo "privacy check denylist must be a regular, non-symlink file" >&2
    exit 2
  fi
  denylist=$(CDPATH='' cd -- "$(dirname "$denylist")" && pwd)/$(basename "$denylist")
  case "$denylist" in
    "$root"/*)
      relative=${denylist#"$root"/}
      if git -C "$root" ls-files --error-unmatch -- "$relative" >/dev/null 2>&1; then
        echo "privacy check denylist must not be tracked" >&2
        exit 2
      fi
      ;;
  esac
  sed -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d' "$denylist" >>"$patterns"
fi

if [ -n "${FLOWLENS_PRIVACY_DENYLIST:-}" ]; then
  printf '%s\n' "$FLOWLENS_PRIVACY_DENYLIST" |
    sed -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d' >>"$patterns"
fi

if [ -s "$patterns" ]; then
  status=0
  grep -z -q -F -f "$patterns" "$tracked" || status=$?
  case "$status" in
    0) fail "a staged path matches the external denylist" ;;
    1) ;;
    *) echo "privacy check could not scan staged paths" >&2; exit 2 ;;
  esac

  status=0
  git -C "$root" grep --cached -q -F -f "$patterns" -- || status=$?
  case "$status" in
    0) fail "staged content matches the external denylist" ;;
    1) ;;
    *) echo "privacy check could not scan staged content" >&2; exit 2 ;;
  esac
fi

echo "privacy check passed"
