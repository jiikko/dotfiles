#!/usr/bin/env bash
# Readiness must never turn a read-only invocation into a build or run an old binary.
set -euo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
fail() { printf '✗ %s\n' "$1" >&2; exit 1; }
ok() { printf '✓ %s\n' "$1"; }
mkdir -p "$TMP_DIR/commands"
for tool in go git xcrun xcode-select; do
  cat > "$TMP_DIR/commands/$tool" <<'SH'
#!/bin/sh
printf '%s\n' "$0 $*" >> "$COMMAND_LOG"
exit 99
SH
  chmod +x "$TMP_DIR/commands/$tool"
done
export PATH="$TMP_DIR/commands:$PATH"
export COMMAND_LOG="$TMP_DIR/commands.log" ARGV_LOG="$TMP_DIR/argv.log"

new_project() {
  PROJECT="$TMP_DIR/$1 worktree with spaces"
  SOURCE="$PROJECT/src/dupscan"
  mkdir -p "$PROJECT/bin/lib" "$SOURCE/sub" "$PROJECT/src/dependency"
  cp "$ROOT_DIR/bin/dupscan" "$PROJECT/bin/dupscan"
  cp "$ROOT_DIR/bin/lib/go_autobuild.zsh" "$PROJECT/bin/lib/go_autobuild.zsh"
  chmod +x "$PROJECT/bin/dupscan"
  printf 'module example/dupscan\ngo 1.26\nreplace example/dependency => ../dependency\n' > "$SOURCE/go.mod"
  printf 'checksum\n' > "$SOURCE/go.sum"
  printf 'package main\n' > "$SOURCE/main.go"
  printf 'package sub\n' > "$SOURCE/sub/sub.go"
  printf 'package sub\n' > "$SOURCE/sub/sub_test.go"
  printf 'module example/dependency\ngo 1.26\n' > "$PROJECT/src/dependency/go.mod"
  printf 'package dependency\n' > "$PROJECT/src/dependency/dependency.go"
  cat > "$SOURCE/dupscan" <<'SH'
#!/bin/sh
printf '%s\000' "$@" > "$ARGV_LOG"
printf 'current binary\n'
exit "${BINARY_RC:-0}"
SH
  chmod +x "$SOURCE/dupscan"
  record_built
}

record_built() {
  zsh -c 'source "$1"; _go_autobuild_fingerprint "$2"; [[ -n "$REPLY" ]] || exit 1; print -rn -- "1"$'"'\n'"'"$REPLY"' \
    -- "$PROJECT/bin/lib/go_autobuild.zsh" "$SOURCE" > "$SOURCE/.autobuild.built"
}

# Logs live outside the project, so any metadata or content change is a side effect.
snapshot() {
  find "$PROJECT" -exec stat -f '%N %m %c %z %p' {} \;
  find "$PROJECT" -type f -exec shasum {} \;
}

run_readonly() {
  snapshot > "$TMP_DIR/before"
  rm -f "$ARGV_LOG" "$COMMAND_LOG"
  RC=0
  "$@" > "$TMP_DIR/stdout" 2> "$TMP_DIR/stderr" </dev/null || RC=$?
  snapshot > "$TMP_DIR/after"
  cmp -s "$TMP_DIR/before" "$TMP_DIR/after" || fail 'no-build changed project files or metadata'
  [[ ! -e "$COMMAND_LOG" ]] || fail "no-build invoked developer tools: $(cat "$COMMAND_LOG")"
}

expect_unready() {
  run_readonly "$PROJECT/bin/dupscan" -no-build index
  [[ "$RC" == 2 ]] || fail "unready binary returned $RC instead of 2: $(cat "$TMP_DIR/stderr")"
  [[ ! -s "$TMP_DIR/stdout" && ! -e "$ARGV_LOG" ]] || fail 'unready binary ran or wrote stdout'
  [[ -s "$TMP_DIR/stderr" ]] || fail 'unready binary gave no diagnostic'
  [[ "$(wc -l < "$TMP_DIR/stderr")" -le 3 && "$(wc -c < "$TMP_DIR/stderr")" -le 1024 ]] || fail 'readiness diagnostic is not short'
  ok "$1"
}

expect_current() {
  run_readonly "$PROJECT/bin/dupscan" -no-build "$@"
  [[ "$RC" == 0 ]] || fail "current binary failed with $RC: $(cat "$TMP_DIR/stderr")"
  [[ "$(cat "$TMP_DIR/stdout")" == 'current binary' && ! -s "$TMP_DIR/stderr" ]] || fail 'current binary output was changed'
  printf '%s\000' "$@" > "$TMP_DIR/expected-argv"
  cmp -s "$TMP_DIR/expected-argv" "$ARGV_LOG" || fail 'wrapper changed binary arguments'
}

new_project current
expect_current index '--' '' 'path with spaces' '$HOME;$(touch unwanted)' '*' $'line\nbreak' '-no-build'
expect_current '--' 'index' '-no-build'
ok 'only the leading -no-build is consumed; remaining argv is preserved exactly'
BINARY_RC=17 run_readonly "$PROJECT/bin/dupscan" -no-build index
[[ "$RC" == 17 ]] || fail 'current binary exit status was changed'
ok 'current binary exit status is preserved'
printf 'unrelated old failure\n' > "$SOURCE/.autobuild.failed"
expect_current index
ok 'an unrelated failure record does not block a current binary'

new_project nonleading
run_readonly "$PROJECT/bin/dupscan" index -no-build -- report
[[ "$RC" == 0 ]] || fail 'nonleading -no-build was treated as a wrapper option'
printf '%s\000' index -no-build -- report > "$TMP_DIR/expected-argv"
cmp -s "$TMP_DIR/expected-argv" "$ARGV_LOG" || fail 'nonleading -no-build was consumed'
ok 'without a leading wrapper option, -no-build is forwarded to the binary'

new_project missing
rm "$SOURCE/dupscan"
expect_unready 'missing binary fails closed without building'
new_project not-executable
chmod -x "$SOURCE/dupscan"
expect_unready 'non-executable binary fails closed'
new_project no-built
rm "$SOURCE/.autobuild.built"
expect_unready 'missing build fingerprint fails closed'
new_project empty-built
: > "$SOURCE/.autobuild.built"
expect_unready 'empty build fingerprint fails closed'
new_project partial-built
printf '1\n' > "$SOURCE/.autobuild.built"
expect_unready 'partial build fingerprint fails closed'

for change in edit add delete mod sum replace replace-mod; do
  new_project "$change"
  case "$change" in
    edit) printf '// changed production source\n' >> "$SOURCE/sub/sub.go" ;;
    add) printf 'package sub\n' > "$SOURCE/sub/added.go" ;;
    delete) rm "$SOURCE/sub/sub.go" ;;
    mod) printf '// changed module\n' >> "$SOURCE/go.mod" ;;
    sum) printf 'another checksum\n' >> "$SOURCE/go.sum" ;;
    replace) printf '// changed dependency\n' >> "$PROJECT/src/dependency/dependency.go" ;;
    replace-mod) printf '// changed dependency module\n' >> "$PROJECT/src/dependency/go.mod" ;;
  esac
  expect_unready "$change in build inputs fails closed"
done
new_project test-only
printf '// test-only edit\n' >> "$SOURCE/sub/sub_test.go"
printf 'package main\n' > "$SOURCE/new_test.go"
expect_current index
ok 'test-only edits and additions do not invalidate the binary'

new_project failed-source
printf '// stale source\n' >> "$SOURCE/main.go"
zsh -c 'source "$1"; _go_autobuild_fingerprint "$2"; print -rn -- "$REPLY"' \
  -- "$PROJECT/bin/lib/go_autobuild.zsh" "$SOURCE" > "$SOURCE/.autobuild.failed"
mkdir "$SOURCE/.autobuild.lock"
printf '999999999\n' > "$SOURCE/.autobuild.lock/pid"
printf 'existing build log\n' > "$SOURCE/.autobuild.log"
expect_unready 'failed current inputs never allow the stale binary, retry, or alter lock/log'
GO_AUTOBUILD_SYNC=1 expect_unready 'GO_AUTOBUILD_SYNC cannot override no-build'
GO_AUTOBUILD_FAILED_TTL=0 expect_unready 'expired failure backoff cannot override no-build'

# No inputs makes the real fingerprint unavailable; the legacy mtime fallback must not apply.
new_project no-inputs
rm "$SOURCE/main.go" "$SOURCE/sub/sub.go" "$SOURCE/go.mod" "$SOURCE/go.sum"
rm -rf "$PROJECT/src/dependency"
expect_unready 'unavailable fingerprint fails closed rather than using mtime fallback'

new_project helper-options
printf '// stale source\n' >> "$SOURCE/main.go"
for order in '--async --no-build' '--no-build --async'; do
  run_readonly zsh -c 'source "$1"; shift; go_autobuild_exec "$@"' \
    -- "$PROJECT/bin/lib/go_autobuild.zsh" ${order} "$SOURCE" dupscan -- index
  [[ "$RC" == 2 && ! -s "$TMP_DIR/stdout" && ! -e "$ARGV_LOG" ]] || fail "helper options $order bypassed no-build"
done
ok 'helper no-build takes precedence over async in either option order'

new_project packages
mkdir -p "$SOURCE/cmd/a" "$SOURCE/cmd/b"
printf 'package main\n' > "$SOURCE/cmd/a/main.go"
printf 'package main\n' > "$SOURCE/cmd/b/main.go"
cp "$SOURCE/dupscan" "$SOURCE/cmd/a/dupscan"
cp "$SOURCE/dupscan" "$SOURCE/cmd/b/dupscan"
record_built
cp "$SOURCE/.autobuild.built" "$SOURCE/cmd/a/.autobuild.built"
for order in '--no-build --async' '--async --no-build'; do
  GO_AUTOBUILD_SYNC=1 run_readonly zsh -c 'source "$1"; shift; go_autobuild_exec "$@"' \
    -- "$PROJECT/bin/lib/go_autobuild.zsh" ${order} --pkg cmd/a "$SOURCE" dupscan -- '--' 'package argument'
  [[ "$RC" == 0 && ! -s "$TMP_DIR/stderr" ]] || fail '--pkg did not execute its current binary'
  printf '%s\000' '--' 'package argument' > "$TMP_DIR/expected-argv"
  cmp -s "$TMP_DIR/expected-argv" "$ARGV_LOG" || fail '--pkg changed binary arguments'
done
run_readonly zsh -c 'source "$1"; shift; go_autobuild_exec "$@"' \
  -- "$PROJECT/bin/lib/go_autobuild.zsh" --pkg cmd/b --no-build "$SOURCE" dupscan -- index
[[ "$RC" == 2 && ! -s "$TMP_DIR/stdout" && ! -e "$ARGV_LOG" ]] || fail '--pkg borrowed another package build record'
printf '// module-wide change\n' >> "$SOURCE/main.go"
run_readonly zsh -c 'source "$1"; shift; go_autobuild_exec "$@"' \
  -- "$PROJECT/bin/lib/go_autobuild.zsh" --no-build --pkg cmd/a "$SOURCE" dupscan -- index
[[ "$RC" == 2 && ! -s "$TMP_DIR/stdout" && ! -e "$ARGV_LOG" ]] || fail '--pkg ignored changed input outside its command directory'
ok 'pkg uses module inputs and an independent binary/build record per command'

new_project first
FIRST_PROJECT="$PROJECT"
printf '// first worktree stale\n' >> "$SOURCE/main.go"
new_project second
expect_current index
PROJECT="$FIRST_PROJECT" SOURCE="$FIRST_PROJECT/src/dupscan"
expect_unready 'readiness belongs to each worktree, independent of another current worktree'
printf '\nOK: dupscan no-build\n'
