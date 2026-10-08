#!/usr/bin/env bash
# go_packages_index.sh の生成・検査契約を実際の Go tool と失敗 shim で固定する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/go_packages_index.sh"
GO_COMMAND="$(command -v go || true)"
if [ -z "$GO_COMMAND" ]; then
	printf '✗ fixture を検証する go が PATH にありません\n' >&2
	exit 1
fi
GO_ROOT="$(GOWORK=off GOENV=off GOFLAGS='' GOTOOLCHAIN=local "$GO_COMMAND" env GOROOT)"
REAL_GO_BIN="$GO_ROOT/bin/go"
[ -x "$REAL_GO_BIN" ] || { printf '✗ go の実体が実行できません: %s\n' "$REAL_GO_BIN" >&2; exit 1; }
ORIGINAL_PATH="$PATH"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-go-packages-index.XXXXXX")" || {
	printf '✗ fixture 用一時ディレクトリを作れません\n' >&2
	exit 1
}
trap 'rm -rf "$WORK"' EXIT
SHIM_DIR="$WORK/shim"
EMPTY_PATH="$WORK/empty-path"
GO_CALL_LOG="$WORK/go-calls.log"
BASE="$WORK/positive"
mkdir -p "$SHIM_DIR" "$EMPTY_PATH"

cat > "$SHIM_DIR/go" <<'SHIM'
#!/bin/sh
printf 'GOWORK=%s GOENV=%s GOFLAGS=%s GOTOOLCHAIN=%s LC_ALL=%s GOOS=%s GOARCH=%s\n' \
	"${GOWORK-<unset>}" "${GOENV-<unset>}" "${GOFLAGS-<unset>}" "${GOTOOLCHAIN-<unset>}" "${LC_ALL-<unset>}" \
	"${GOOS-<unset>}" "${GOARCH-<unset>}" >> "$GO_CALL_LOG"
tagged_call=0
for arg in "$@"; do
	if [ "$arg" = '-tags' ]; then
		tagged_call=1
		break
	fi
done
if [ "$tagged_call" -eq 1 ]; then
	case "${FAKE_GO_MODE:-delegate}" in
		tagged-rc)
			printf 'shim: tagged go list failure\n' >&2
			exit 23
			;;
		tagged-wrong-dir)
			printf 'fixture.test/wrong\037%s/elsewhere\037tagonly\037Package tagonly is a fixture package.\037\n' "$PWD"
			exit 0
			;;
	esac
fi
case "${FAKE_GO_MODE:-delegate}" in
	rc)
		printf 'shim: requested go list failure\n' >&2
		exit 23
		;;
	error)
		printf 'fixture.test/demo\037%s\037demo\037Package demo is a fixture package.\037synthetic go list error\n' "$PWD"
		exit 0
		;;
	whitespace-doc)
		printf 'fixture.test/demo\037%s\037demo\037   \037\n' "$PWD"
		exit 0
		;;
	no-output)
		exit 0
		;;
esac
exec "$REAL_GO_BIN" "$@"
SHIM
chmod +x "$SHIM_DIR/go"

ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; FAILS=$((FAILS + 1)); }
FAILS=0
RC=0
OUT=''

new_fixture() {
	local destination="$1"
	mkdir -p "$destination/scripts" "$destination/src/demo/cmd/hello" \
		"$destination/src/demo/internal/helper" "$destination/src/demo/tagonly" \
		"$destination/src/demo/vendor/thirdparty" "$destination/src/second"
	cp "$SCRIPT" "$destination/scripts/go_packages_index.sh"
	cat > "$destination/src/demo/go.mod" <<'GO_MOD'
module fixture.test/demo

go 1.25.0
GO_MOD
	cat > "$destination/src/demo/doc.go" <<'GO_DOC'
// Package demo is a fixture package.
package demo
GO_DOC
	cat > "$destination/src/demo/cmd/hello/main.go" <<'GO_MAIN'
// Command hello prints a greeting.
package main
GO_MAIN
	cat > "$destination/src/demo/internal/helper/helper.go" <<'GO_HELPER'
// Package helper contains an internal fixture helper.
package helper
GO_HELPER
	cat > "$destination/src/demo/tagonly/tag.go" <<'GO_TAGGED'
//go:build feature

// Package tagonly is available when the feature build tag is set.
package tagonly
GO_TAGGED
	cat > "$destination/src/demo/vendor/thirdparty/vendor.go" <<'GO_VENDOR'
package thirdparty
GO_VENDOR
	cat > "$destination/src/second/go.mod" <<'GO_MOD_SECOND'
module fixture.test/second

go 1.25.0
GO_MOD_SECOND
	cat > "$destination/src/second/doc.go" <<'GO_DOC_SECOND'
// Package second is a second fixture module.
package second
GO_DOC_SECOND
}

run_fixture() {
	local fixture="$1"
	local fake_mode="$2"
	shift 2
	RC=0
	OUT="$(
		cd "$fixture" &&
		env PATH="$SHIM_DIR:$ORIGINAL_PATH" REAL_GO_BIN="$REAL_GO_BIN" GO_CALL_LOG="$GO_CALL_LOG" \
			FAKE_GO_MODE="$fake_mode" GOWORK="$WORK/ignored.work" GOENV="$WORK/ignored-goenv" \
			GOFLAGS='-tags=wrong' GOOS=windows GOARCH=386 LC_ALL=POSIX \
			/bin/bash "$fixture/scripts/go_packages_index.sh" "$@" 2>&1
	)" || RC=$?
}

expect_failure() {
	local name="$1"
	local reason="$2"
	if [ "$RC" -ne 0 ] && grep -Fq "$reason" <<< "$OUT"; then
		ok "$name (rc=$RC)"
	else
		bad "$name を理由つきで落とさない (rc=$RC): $OUT"
	fi
}

printf 'Test 1: 有効な package を生成し、生成物を --check で再検査する\n'
new_fixture "$BASE"
run_fixture "$BASE" delegate
if [ "$RC" -eq 0 ] && grep -Fq '2 modules / 5 packages' <<< "$OUT"; then
	ok '生成が成功し、module と package 数を表示する'
else
	bad "fixture の生成が失敗した (rc=$RC): $OUT"
fi
if grep -Fq -- "- \`src/demo\` (\`fixture.test/demo\`) — Package demo is a fixture package." "$BASE/src/PACKAGES.md"; then
	ok '通常 package の import path と synopsis を生成する'
else
	bad '通常 package の import path または synopsis が無い'
fi
if grep -Fq -- "- \`src/demo/internal/helper\` (\`fixture.test/demo/internal/helper\`) — Package helper contains an internal fixture helper. (internal)" "$BASE/src/PACKAGES.md"; then
	ok 'internal package に印を付ける'
else
	bad 'internal package の印が無い'
fi
if grep -Fq -- "- \`src/second\` (\`fixture.test/second\`) — Package second is a second fixture module." "$BASE/src/PACKAGES.md"; then
	ok '2 つ目の module の package を出力する'
else
	bad '2 つ目の module の package 行が無い'
fi
if grep -Fq -- "- \`src/demo/cmd/hello\` (\`fixture.test/demo/cmd/hello\`) — Command hello prints a greeting. (main)" "$BASE/src/PACKAGES.md"; then
	ok 'main package に印を付ける'
else
	bad 'main package の印が無い'
fi
if grep -Fq -- "- \`src/demo/tagonly\` (\`fixture.test/demo/tagonly\`) — Package tagonly is available when the feature build tag is set. (build tags: feature)" "$BASE/src/PACKAGES.md"; then
	ok 'tag 専用 package を再取得して印を付ける'
else
	bad 'tag 専用 package の行が無いか tag 印が無い'
fi
entry_count="$(grep -c '^- `' "$BASE/src/PACKAGES.md" || true)"
if [ "$entry_count" -eq 5 ]; then
	ok '2 module の母集合にある 5 package をすべて出力する (vendor は除外)'
else
	bad "package 数が 5 でない: $entry_count"
fi
run_fixture "$BASE" delegate --check
if [ "$RC" -eq 0 ]; then
	ok '--check が生成物を受け入れる'
else
	bad "--check が失敗した (rc=$RC): $OUT"
fi
if grep -Fqx 'GOWORK=off GOENV=off GOFLAGS= GOTOOLCHAIN=local LC_ALL=C GOOS=<unset> GOARCH=<unset>' "$GO_CALL_LOG"; then
	ok 'go list の環境と GOTOOLCHAIN を固定し、GOOS / GOARCH は実行機の既定にする'
else
	bad "go list の環境固定がログに無い: $(cat "$GO_CALL_LOG")"
fi

printf 'Test 2: package doc の不備を理由つきで落とす\n'
EMPTY_DOC="$WORK/empty-doc"
mkdir -p "$EMPTY_DOC"
cp -R "$BASE/." "$EMPTY_DOC/"
cat > "$EMPTY_DOC/src/demo/doc.go" <<'EMPTY_DOC_GO'
//
package demo
EMPTY_DOC_GO
run_fixture "$EMPTY_DOC" delegate
expect_failure 'doc comment 不足' 'package doc comment が空です: src/demo'
run_fixture "$BASE" whitespace-doc
expect_failure '空白だけの .Doc' 'package doc comment が空です: src/demo'

WRONG_PREFIX="$WORK/wrong-prefix"
mkdir -p "$WRONG_PREFIX"
cp -R "$BASE/." "$WRONG_PREFIX/"
cat > "$WRONG_PREFIX/src/demo/doc.go" <<'WRONG_PREFIX_GO'
// Demo is a fixture package.
package demo
WRONG_PREFIX_GO
run_fixture "$WRONG_PREFIX" delegate
expect_failure '非 main の Package prefix 不足' "非 main package の doc は 'Package demo ' で始めてください: src/demo"

TAG_DOC_MISSING="$WORK/tag-doc-missing"
mkdir -p "$TAG_DOC_MISSING"
cp -R "$BASE/." "$TAG_DOC_MISSING/"
cat > "$TAG_DOC_MISSING/src/demo/tagonly/tag.go" <<'TAG_NO_DOC'
//go:build feature

package tagonly
TAG_NO_DOC
run_fixture "$TAG_DOC_MISSING" delegate
expect_failure 'tag 専用 package の doc 不足' 'package doc comment が空です: src/demo/tagonly'

TAG_NEGATION="$WORK/tag-negation"
mkdir -p "$TAG_NEGATION"
cp -R "$BASE/." "$TAG_NEGATION/"
cat > "$TAG_NEGATION/src/demo/tagonly/tag.go" <<'TAG_NEGATION_GO'
//go:build feature && !other

package tagonly
TAG_NEGATION_GO
cat > "$TAG_NEGATION/src/demo/tagonly/other.go" <<'TAG_OTHER_GO'
//go:build other

// Package tagonly is available when the other build tag is set.
package tagonly
TAG_OTHER_GO
run_fixture "$TAG_NEGATION" delegate
expect_failure '否定を含む build tag' '否定を含む //go:build 行は未対応です: src/demo/tagonly'

SYMLINK_DOC_MISSING="$WORK/symlink-doc-missing"
mkdir -p "$SYMLINK_DOC_MISSING"
cp -R "$BASE/." "$SYMLINK_DOC_MISSING/"
mkdir -p "$SYMLINK_DOC_MISSING/src/demo/symlinkonly" "$SYMLINK_DOC_MISSING/src/demo/testdata"
printf 'package symlinkonly\n' > "$SYMLINK_DOC_MISSING/src/demo/testdata/source.txt"
ln -s ../testdata/source.txt "$SYMLINK_DOC_MISSING/src/demo/symlinkonly/link.go"
run_fixture "$SYMLINK_DOC_MISSING" delegate
expect_failure 'symlink だけの package の doc 不足' 'package doc comment が空です: src/demo/symlinkonly'

BROKEN_SYMLINK="$WORK/broken-symlink"
mkdir -p "$BROKEN_SYMLINK"
cp -R "$BASE/." "$BROKEN_SYMLINK/"
mkdir -p "$BROKEN_SYMLINK/src/demo/brokenlink"
ln -s missing-source.txt "$BROKEN_SYMLINK/src/demo/brokenlink/missing.go"
run_fixture "$BROKEN_SYMLINK" delegate
expect_failure '参照先が読めない Go symlink' 'Go source symlink の参照先を読めません: src/demo/brokenlink/missing.go'

printf 'Test 3: 生成物の差分、nested module、Go tool の失敗を緑にしない\n'
MANUAL="$WORK/manual-index"
mkdir -p "$MANUAL"
cp -R "$BASE/." "$MANUAL/"
cp "$MANUAL/src/PACKAGES.md" "$WORK/manual-before.md"
sed 's/fixture package/changed package/' "$MANUAL/src/PACKAGES.md" > "$WORK/manual-edited.md"
mv "$WORK/manual-edited.md" "$MANUAL/src/PACKAGES.md"
before_bytes="$(wc -c < "$WORK/manual-before.md" | tr -d '[:space:]')"
after_bytes="$(wc -c < "$MANUAL/src/PACKAGES.md" | tr -d '[:space:]')"
before_lines="$(wc -l < "$WORK/manual-before.md" | tr -d '[:space:]')"
after_lines="$(wc -l < "$MANUAL/src/PACKAGES.md" | tr -d '[:space:]')"
if [ "$before_bytes" -eq "$after_bytes" ] && [ "$before_lines" -eq "$after_lines" ]; then
	ok '既存行を書き換え、行数と byte 数を維持する'
else
	bad '手編集 fixture が行数または byte 数を維持していない'
fi
run_fixture "$MANUAL" delegate --check
expect_failure '同じ長さの PACKAGES.md 手編集' 'src/PACKAGES.md is stale'

SECOND_DOC_MISSING="$WORK/second-doc-missing"
mkdir -p "$SECOND_DOC_MISSING"
cp -R "$BASE/." "$SECOND_DOC_MISSING/"
cat > "$SECOND_DOC_MISSING/src/second/doc.go" <<'SECOND_DOC_MISSING_GO'
//
package second
SECOND_DOC_MISSING_GO
run_fixture "$SECOND_DOC_MISSING" delegate
expect_failure '2 つ目の module の doc 不足' 'package doc comment が空です: src/second'

NEW_MODULE="$WORK/new-module-stale"
mkdir -p "$NEW_MODULE"
cp -R "$BASE/." "$NEW_MODULE/"
mkdir -p "$NEW_MODULE/src/third"
cat > "$NEW_MODULE/src/third/go.mod" <<'GO_MOD_THIRD'
module fixture.test/third

go 1.25.0
GO_MOD_THIRD
cat > "$NEW_MODULE/src/third/doc.go" <<'GO_DOC_THIRD'
// Package third is a newly added fixture module.
package third
GO_DOC_THIRD
run_fixture "$NEW_MODULE" delegate --check
expect_failure '新 module 追加後の古い一覧' 'src/PACKAGES.md is stale'

NESTED="$WORK/nested-module"
mkdir -p "$NESTED"
cp -R "$BASE/." "$NESTED/"
mkdir -p "$NESTED/src/demo/nested"
printf 'module fixture.test/demo/nested\n\ngo 1.25.0\n' > "$NESTED/src/demo/nested/go.mod"
run_fixture "$NESTED" delegate
expect_failure 'nested module' 'nested Go module は未対応です: src/demo/nested'

run_fixture "$BASE" rc
expect_failure 'go list の非ゼロ rc' 'go list が失敗しました: src/demo (rc=23)'
run_fixture "$BASE" tagged-rc
expect_failure 'tag 付き go list の非ゼロ rc' 'build tag 付き go list が失敗しました: src/demo/tagonly (rc=23)'
run_fixture "$BASE" tagged-wrong-dir
expect_failure 'tag 付き go list の directory 不一致' 'go list の directory が一致しません: src/demo/tagonly'
run_fixture "$BASE" error
expect_failure 'go list の .Error' 'go list の .Error が空ではありません: src/demo (synthetic go list error)'
run_fixture "$BASE" no-output
expect_failure 'go list が何も出さない' 'go list が package 情報を返しませんでした: src/demo'

printf 'Test 4: module が package 0 件 / go 不在を緑にしない\n'
ZERO="$WORK/zero-packages"
mkdir -p "$ZERO/scripts" "$ZERO/src/empty"
cp "$SCRIPT" "$ZERO/scripts/go_packages_index.sh"
printf 'module fixture.test/empty\n\ngo 1.25.0\n' > "$ZERO/src/empty/go.mod"
printf 'package empty\n' > "$ZERO/src/empty/empty_test.go"
run_fixture "$ZERO" delegate
expect_failure 'module の package 0 件' 'module の package が 0 件です: src/empty'

RC=0
OUT="$(cd "$BASE" && PATH="$EMPTY_PATH" /bin/bash "$BASE/scripts/go_packages_index.sh" 2>&1)" || RC=$?
expect_failure 'go が PATH に無い' 'go が PATH にありません (Go package index は skip しません)'

if [ "$FAILS" -gt 0 ]; then
	printf '\n✗ go_packages_index の fixture test: %s 件失敗\n' "$FAILS" >&2
	exit 1
fi
printf '\n✓ go_packages_index の fixture test がすべて成功しました\n'
