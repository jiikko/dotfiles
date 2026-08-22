#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify クリップボード入力テスト
# 引数なし呼び出しでクリップボードを読み、確認を取ってから処理する経路の検証。
#
# TTY 判定は __av1ify_stdin_is_tty() を差し替えて決定論的にする (テストの stdin は
# 実行環境によって TTY にもパイプにもなるため、実際の -t 0 に依存させない)。

source "${0:A:h}/test_helper.sh"

# ファイル名の $(...) が実行される条件 (issue 089) を再現するため、対話シェルと同じく
# prompt 展開を有効にした状態で走らせる。
setopt prompt_subst

printf '\n=== av1ify Clipboard Tests ===\n\n'

# pbpaste モック: MOCK_CLIPBOARD の内容を返す。呼ばれた事実は PBPASTE_LOG に残す
# (「引数があるときはクリップボードを読まない」の検証に使う)。
cat > "$MOCK_BIN_DIR/pbpaste" <<'EOF'
#!/usr/bin/env sh
if [ -n "${PBPASTE_LOG-}" ]; then echo called >> "$PBPASTE_LOG"; fi
if [ -n "${MOCK_PBPASTE_FAIL-}" ]; then exit 1; fi
printf '%s\n' "${MOCK_CLIPBOARD-}"
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/pbpaste"

# TTY 判定の差し替え (既定は「対話」= クリップボード経路を発火させる)
__av1ify_stdin_is_tty() { return 0 }

# 引数なし av1ify を実行し、$1 を確認プロンプトへの回答として渡す
# 出力: CLIP_OUTPUT (stdout+stderr), CLIP_RC (終了コード)
run_av1ify_clip() {
  local answer="$1"
  unsetopt err_exit
  CLIP_OUTPUT=$(av1ify 2>&1 <<< "$answer")
  CLIP_RC=$?
  setopt err_exit
}

# Test 1: y で承認 → 存在するパスだけ変換され、不在パスは除外表示される
printf '## Test 1: Approve with y (missing paths excluded)\n'
TEST_DIR="$TEST_TMP/clip1"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/a.avi"
echo "dummy video" > "$TEST_DIR/b.avi"
cd "$TEST_DIR"
export MOCK_CLIPBOARD="$TEST_DIR/a.avi
$TEST_DIR/b.avi
$TEST_DIR/missing.avi"
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "クリップボードから読み取りました" "Announces clipboard read"
assert_contains "$CLIP_OUTPUT" "✓ $TEST_DIR/a.avi" "Lists existing path with check mark"
assert_contains "$CLIP_OUTPUT" "✗ $TEST_DIR/missing.avi" "Lists missing path with cross mark"
assert_contains "$CLIP_OUTPUT" "除外" "Says missing paths are excluded"
assert_contains "$CLIP_OUTPUT" "対象 2件 / 除外 1件" "Summarizes target/excluded counts"
assert_file_exists "$TEST_DIR/a-enc.mp4" "Converts first clipboard path"
assert_file_exists "$TEST_DIR/b-enc.mp4" "Converts second clipboard path"
[[ "$CLIP_RC" -eq 0 ]] && printf '✓ Exit code is 0 on approval\n' || { printf '✗ Exit code is 0 on approval (got %s)\n' "$CLIP_RC"; exit 1; }

# Test 2: 空回答 (Enter) → 中止。ファイルは作られない
printf '\n## Test 2: Decline with empty answer\n'
TEST_DIR="$TEST_TMP/clip2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/a.avi"
cd "$TEST_DIR"
export MOCK_CLIPBOARD="$TEST_DIR/a.avi"
run_av1ify_clip ""
assert_contains "$CLIP_OUTPUT" "中止しました" "Reports cancellation"
assert_file_not_exists "$TEST_DIR/a-enc.mp4" "Does not convert when declined"
[[ "$CLIP_RC" -eq 130 ]] && printf '✓ Exit code is 130 on cancel\n' || { printf '✗ Exit code is 130 on cancel (got %s)\n' "$CLIP_RC"; exit 1; }

# Test 3: 非対話 (TTY でない) → 従来どおりヘルプ。クリップボードは読まない
printf '\n## Test 3: Non-interactive falls back to help\n'
TEST_DIR="$TEST_TMP/clip3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/a.avi"
cd "$TEST_DIR"
export MOCK_CLIPBOARD="$TEST_DIR/a.avi"
export PBPASTE_LOG="$TEST_DIR/pbpaste.log"
__av1ify_stdin_is_tty() { return 1 }
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "使い方" "Shows help when not a TTY"
assert_file_not_exists "$TEST_DIR/a-enc.mp4" "Does not convert when not a TTY"
assert_file_not_exists "$PBPASTE_LOG" "Does not call pbpaste when not a TTY"
__av1ify_stdin_is_tty() { return 0 }
unset PBPASTE_LOG

# Test 4: クリップボードが空 → エラー
printf '\n## Test 4: Empty clipboard is an error\n'
TEST_DIR="$TEST_TMP/clip4"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
export MOCK_CLIPBOARD=""
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "クリップボードが空です" "Reports empty clipboard"
[[ "$CLIP_RC" -eq 1 ]] && printf '✓ Exit code is 1 on empty clipboard\n' || { printf '✗ Exit code is 1 on empty clipboard (got %s)\n' "$CLIP_RC"; exit 1; }

# Test 5: 有効なパスが 0 件 → エラー (確認プロンプトまで進まない)
printf '\n## Test 5: No resolvable path is an error\n'
TEST_DIR="$TEST_TMP/clip5"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
export MOCK_CLIPBOARD="$TEST_DIR/nope1.avi
$TEST_DIR/nope2.avi"
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "有効なパスを読み取れませんでした" "Reports when nothing resolves"
assert_not_contains "$CLIP_OUTPUT" "これを入力にしますか" "Does not prompt when nothing resolves"
[[ "$CLIP_RC" -eq 1 ]] && printf '✓ Exit code is 1 when nothing resolves\n' || { printf '✗ Exit code is 1 when nothing resolves (got %s)\n' "$CLIP_RC"; exit 1; }

# Test 6: 引用符・バックスラッシュエスケープ・前後の空白を剥がして解決する
printf '\n## Test 6: Unquote and unescape pasted paths\n'
TEST_DIR="$TEST_TMP/clip6"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/space name.avi"
echo "dummy video" > "$TEST_DIR/quoted.avi"
cd "$TEST_DIR"
export MOCK_CLIPBOARD="  ${TEST_DIR}/space\\ name.avi
'$TEST_DIR/quoted.avi'"
run_av1ify_clip y
assert_file_exists "$TEST_DIR/space name-enc.mp4" "Resolves backslash-escaped space"
assert_file_exists "$TEST_DIR/quoted-enc.mp4" "Resolves single-quoted path"

# Test 7: ファイル名の $(...) を実行しない (issue 089 の同型を新経路に持ち込まない)
printf '\n## Test 7: Command substitution in filename is not executed\n'
TEST_DIR="$TEST_TMP/clip7"
mkdir -p "$TEST_DIR"
# スラッシュを含まない名前にする (ファイル名に / は入れられない)。実行されると
# cwd (= TEST_DIR) に pwned が作られるので、cd 後に作成する。
cd "$TEST_DIR"
EVIL_NAME='evil$(touch pwned)file.avi'
echo "dummy video" > "./$EVIL_NAME"
export MOCK_CLIPBOARD="$TEST_DIR/$EVIL_NAME"
# 一覧表示だけを検証対象にするため n で中止する (処理本体の print -P は issue 089 の管轄)
run_av1ify_clip n
assert_file_not_exists "$TEST_DIR/pwned" "Does not execute \$(...) embedded in pasted filename"
assert_contains "$CLIP_OUTPUT" "$EVIL_NAME" "Shows the pasted filename verbatim"

# Test 8: 引数がある呼び出しではクリップボードを読まない
printf '\n## Test 8: Arguments suppress clipboard read\n'
TEST_DIR="$TEST_TMP/clip8"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/arg.avi"
echo "dummy video" > "$TEST_DIR/clip.avi"
cd "$TEST_DIR"
export MOCK_CLIPBOARD="$TEST_DIR/clip.avi"
export PBPASTE_LOG="$TEST_DIR/pbpaste.log"
av1ify "$TEST_DIR/arg.avi" > /dev/null 2>&1
assert_file_exists "$TEST_DIR/arg-enc.mp4" "Converts the explicit argument"
assert_file_not_exists "$TEST_DIR/clip-enc.mp4" "Ignores clipboard when arguments are given"
assert_file_not_exists "$PBPASTE_LOG" "Does not call pbpaste when arguments are given"
# --help も同様 (クリップボードを読まずヘルプを出す)
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "クリップボード" "Help documents the clipboard input"
assert_file_not_exists "$PBPASTE_LOG" "Does not call pbpaste for --help"
unset PBPASTE_LOG

# Test 9: pbpaste が失敗したらエラーとして報告する (沈黙して help に落ちない)
printf '\n## Test 9: pbpaste failure is reported\n'
TEST_DIR="$TEST_TMP/clip9"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
export MOCK_PBPASTE_FAIL=1
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "クリップボードの読み取りに失敗" "Reports pbpaste failure"
[[ "$CLIP_RC" -eq 1 ]] && printf '✓ Exit code is 1 on pbpaste failure\n' || { printf '✗ Exit code is 1 on pbpaste failure (got %s)\n' "$CLIP_RC"; exit 1; }
unset MOCK_PBPASTE_FAIL

printf '\n=== av1ify Clipboard Tests: all passed ===\n'
