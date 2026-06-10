#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify NG 一覧テスト
# バッチ処理 (-f / multi-arg / directory) で失敗したファイルが
# サマリ末尾に「NG 一覧」として列挙されることを検証する。

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify NG List Tests ===\n\n'

# Test 1: multi-arg で全件 NG → NG 一覧にすべて列挙される
printf '## Test 1: Multi-arg all-missing -> NG list lists every file with reason\n'
TEST_DIR="$TEST_TMP/ng_test1"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/missing_a.avi" "$TEST_DIR/missing_b.mkv" "$TEST_DIR/missing_c.wmv" 2>&1 || true)
setopt err_exit
assert_contains "$output" "NG=3" "Summary reports NG=3"
assert_contains "$output" "── NG 一覧 (3件) ──" "NG header shows count"
assert_contains "$output" "missing_a.avi" "NG list contains first file"
assert_contains "$output" "missing_b.mkv" "NG list contains second file"
assert_contains "$output" "missing_c.wmv" "NG list contains third file"
assert_contains "$output" "ファイルが見つからない" "NG list shows reason"

# Test 2: 全件成功 → NG 一覧は出ない
# モック ffmpeg は出力に "mock\n" (5B) を書くため、ソースを 5B より大きくして
# postcheck の "サイズ増加" 検出を回避する
printf '\n## Test 2: All success -> NG list is NOT printed\n'
TEST_DIR="$TEST_TMP/ng_test2"
mkdir -p "$TEST_DIR"
echo "video content data" > "$TEST_DIR/ok1.avi"
echo "video content data" > "$TEST_DIR/ok2.mkv"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/ok1.avi" "$TEST_DIR/ok2.mkv" 2>&1 || true)
setopt err_exit
assert_contains "$output" "NG=0" "Summary reports NG=0"
assert_not_contains "$output" "NG 一覧" "NG list header is absent when no failures"

# Test 3: OK + NG 混在 → NG 一覧には NG ファイルだけが載る
printf '\n## Test 3: Mixed OK+NG -> NG list contains only failed files\n'
TEST_DIR="$TEST_TMP/ng_test3"
mkdir -p "$TEST_DIR"
echo "video content data" > "$TEST_DIR/works.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/works.avi" "$TEST_DIR/missing.mkv" 2>&1 || true)
setopt err_exit
assert_contains "$output" "OK=1" "Summary OK=1"
assert_contains "$output" "NG=1" "Summary NG=1"
assert_contains "$output" "── NG 一覧 (1件) ──" "NG header count is 1"
assert_contains "$output" "missing.mkv" "NG list contains failed file"
# 成功ファイルは「---- 処理: ...」に出るが NG 一覧の '✗ ' プレフィクスでは出ない
ng_section="${output##*── NG 一覧*}"
assert_not_contains "$ng_section" "works.avi" "Successful file is NOT listed in NG section"

# Test 4: -f モードでも NG 一覧が表示される
printf '\n## Test 4: -f mode shows NG list\n'
TEST_DIR="$TEST_TMP/ng_test4"
mkdir -p "$TEST_DIR"
cat > "$TEST_DIR/list.txt" <<LISTEOF
$TEST_DIR/no1.avi
$TEST_DIR/no2.mkv
LISTEOF
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify -f "$TEST_DIR/list.txt" 2>&1 || true)
setopt err_exit
assert_contains "$output" "── NG 一覧 (2件) ──" "-f mode shows NG header"
assert_contains "$output" "no1.avi" "-f mode lists first NG file"
assert_contains "$output" "no2.mkv" "-f mode lists second NG file"
assert_contains "$output" "ファイルが見つからない" "-f mode shows reason"

# Test 5: 単一ファイル (バッチではない) → NG 一覧は出ない
printf '\n## Test 5: Single file failure does NOT print NG list (no batch summary)\n'
TEST_DIR="$TEST_TMP/ng_test5"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/lone_missing.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ファイルが無い" "Single-file mode still prints inline error"
assert_not_contains "$output" "NG 一覧" "Single-file mode does NOT print NG list section"
assert_not_contains "$output" "サマリ" "Single-file mode does NOT print batch summary"

# Test 6: NG 行は '  ✗ <file>' と '    └─ <reason>' の 2 行構成
printf '\n## Test 6: NG entry has 2-line format (file then reason)\n'
TEST_DIR="$TEST_TMP/ng_test6"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/x.avi" "$TEST_DIR/y.mkv" 2>&1 || true)
setopt err_exit
assert_contains "$output" "  ✗ ${TEST_DIR}/x.avi" "File line uses '  ✗ ' prefix"
assert_contains "$output" "    └─ ファイルが見つからない" "Reason line uses '    └─ ' prefix"

# Test 7: ディレクトリ走査でも NG 一覧が出る (壊れたファイルは ffprobe を素通りする
# 軽い構造を用いるとモック ffmpeg が成功してしまうため、ここではモックを失敗側に
# 上書きして ffmpeg encode failure を起こす)
printf '\n## Test 7: Directory mode shows NG list on ffmpeg failure\n'
TEST_DIR="$TEST_TMP/ng_test7"
mkdir -p "$TEST_DIR"
echo "v" > "$TEST_DIR/a.avi"
echo "v" > "$TEST_DIR/b.mkv"

# 失敗版 ffmpeg モック
FAIL_BIN="$TEST_DIR/fail_bin"
mkdir -p "$FAIL_BIN"
cat > "$FAIL_BIN/ffmpeg" <<'MOCKEOF'
#!/usr/bin/env sh
# -h は成功 (encoder 検出時)
for arg in "$@"; do
  case "$arg" in -h) exit 0 ;; esac
done
exit 1
MOCKEOF
chmod +x "$FAIL_BIN/ffmpeg"
cd "$TEST_DIR"
unsetopt err_exit
# zsh の VAR=val funcname では PATH が後続テストにリークする場合があるため、
# 明示的に save/restore する
__saved_path="$PATH"
PATH="$FAIL_BIN:$PATH"
output=$(av1ify "$TEST_DIR" 2>&1 || true)
PATH="$__saved_path"
unset __saved_path
setopt err_exit
assert_contains "$output" "── NG 一覧" "Directory mode prints NG header on encode failure"
assert_contains "$output" "a.avi" "Directory mode lists failed file a"
assert_contains "$output" "b.mkv" "Directory mode lists failed file b"
assert_contains "$output" "ffmpeg エンコード失敗" "Directory mode shows ffmpeg failure reason"

# Test 8: postcheck NG (サイズ増加) でも NG 一覧に理由が出る
# モック ffmpeg は 5B の出力を吐くので、ソース 1B にしてサイズ増加を強制発火
printf '\n## Test 8: postcheck NG reason is reported in NG list\n'
TEST_DIR="$TEST_TMP/ng_test8"
mkdir -p "$TEST_DIR"
printf 'x' > "$TEST_DIR/tiny1.avi"
printf 'x' > "$TEST_DIR/tiny2.mkv"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/tiny1.avi" "$TEST_DIR/tiny2.mkv" 2>&1 || true)
setopt err_exit
assert_contains "$output" "── NG 一覧 (2件) ──" "postcheck failures contribute to NG count"
assert_contains "$output" "変換後チェック NG" "Reason starts with 'postcheck NG' label"
assert_contains "$output" "サイズ増加" "Reason contains specific issue tag"

printf '\n=== NG List Tests Completed ===\n'
