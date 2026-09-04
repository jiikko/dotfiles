#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify クリップボード入力テスト
# 引数なし呼び出しでクリップボードを読み、確認を取ってから処理する経路の検証。
#
# 発火ゲート (__av1ify_clipboard_mode_available) は差し替えて決定論的にする。
# 差し替えない「本番のゲートそのもの」の検証は Test 10-12 で行う (差し替えテストだけでは
# 「ゲートがスクリプト文脈で偽になるか」を一切 pin できず、後方互換の主張が空振りになる)。

source "${0:A:h}/test_helper.sh"

# ファイル名の $(...) が実行される条件 (issue 089) を再現するため、対話シェルと同じく
# prompt 展開を有効にした状態で走らせる。
setopt prompt_subst

# mktemp -d は macOS では /var/folders/... (= /private/var/... への symlink) を返す。
# 一覧表示は :A で symlink を解決した実体を出すため、期待値も解決後に揃えておく
# (揃えないと全行に「(貼付: ...)」が付き、symlink を貼ったケースとの区別が付かない)。
TEST_TMP="${TEST_TMP:A}"

ORIG_HOME="$HOME"

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

# 発火ゲートの差し替え (既定は「発火する」)
__av1ify_clipboard_mode_available() { return 0 }

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
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/a.avi
$TEST_DIR/b.avi
$TEST_DIR/missing.avi"
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "クリップボードのテキストから読み取りました" "Announces clipboard read and which flavor it used"
assert_contains "$CLIP_OUTPUT" "✓ $TEST_DIR/a.avi" "Lists existing path with check mark"
assert_contains "$CLIP_OUTPUT" "✗ $TEST_DIR/missing.avi" "Lists missing path with cross mark"
assert_contains "$CLIP_OUTPUT" "除外" "Says missing paths are excluded"
assert_contains "$CLIP_OUTPUT" "対象 2件 / 除外 1件" "Summarizes target/excluded counts"
assert_contains "$CLIP_OUTPUT" "これを入力にしますか" "Asks for confirmation"
# 一覧とプロンプトは stderr へ出す (stdout だと `av1c > log` で端末に何も見えないまま入力待ちになる)
unsetopt err_exit
stdout_only=$(av1ify 2>/dev/null <<< "n")
setopt err_exit
assert_not_contains "$stdout_only" "これを入力にしますか" "Prompt goes to stderr, not stdout"
assert_not_contains "$stdout_only" "$TEST_DIR/a.avi" "Listing goes to stderr, not stdout"
assert_file_exists "$TEST_DIR/a-enc.mp4" "Converts first clipboard path"
assert_file_exists "$TEST_DIR/b-enc.mp4" "Converts second clipboard path"
[[ "$CLIP_RC" -eq 0 ]] && printf '✓ Exit code is 0 on approval\n' || { printf '✗ Exit code is 0 on approval (got %s)\n' "$CLIP_RC"; exit 1; }

# Test 2: 空回答 (Enter) → 中止。ファイルは作られない
printf '\n## Test 2: Decline with empty answer\n'
TEST_DIR="$TEST_TMP/clip2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/a.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/a.avi"
run_av1ify_clip ""
assert_contains "$CLIP_OUTPUT" "中止しました" "Reports cancellation"
assert_file_not_exists "$TEST_DIR/a-enc.mp4" "Does not convert when declined"
[[ "$CLIP_RC" -eq 130 ]] && printf '✓ Exit code is 130 on cancel\n' || { printf '✗ Exit code is 130 on cancel (got %s)\n' "$CLIP_RC"; exit 1; }

# Test 3: ゲートが閉じている → 従来どおりヘルプ。クリップボードは読まない
printf '\n## Test 3: Closed gate falls back to help\n'
TEST_DIR="$TEST_TMP/clip3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/a.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/a.avi"
export PBPASTE_LOG="$TEST_DIR/pbpaste.log"
__av1ify_clipboard_mode_available() { return 1 }
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "使い方" "Shows help when the gate is closed"
assert_file_not_exists "$TEST_DIR/a-enc.mp4" "Does not convert when the gate is closed"
assert_file_not_exists "$PBPASTE_LOG" "Does not call pbpaste when the gate is closed"
__av1ify_clipboard_mode_available() { return 0 }
unset PBPASTE_LOG

# Test 4: クリップボードが空 → エラー
printf '\n## Test 4: Empty clipboard is an error\n'
TEST_DIR="$TEST_TMP/clip4"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD=""
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "クリップボードが空です" "Reports empty clipboard"
[[ "$CLIP_RC" -eq 1 ]] && printf '✓ Exit code is 1 on empty clipboard\n' || { printf '✗ Exit code is 1 on empty clipboard (got %s)\n' "$CLIP_RC"; exit 1; }

# Test 5: 有効なパスが 0 件 → エラー (確認プロンプトまで進まない)
printf '\n## Test 5: No resolvable path is an error\n'
TEST_DIR="$TEST_TMP/clip5"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="  ${TEST_DIR}/space\\ name.avi
'$TEST_DIR/quoted.avi'"
run_av1ify_clip y
assert_file_exists "$TEST_DIR/space name-enc.mp4" "Resolves backslash-escaped space"
assert_file_exists "$TEST_DIR/quoted-enc.mp4" "Resolves single-quoted path"

# Test 7: 一覧表示ではファイル名の $(...) を実行しない
# 🚨 主張はここまで。**承認 (y) した後**は処理本体 (_av1ify_encode.zsh の print -P 群) が
#    実行してしまう = 未修正の issue 089 の管轄。この経路をこのテストは検証していない
#    (red team が y 版で pwned 生成を実測済み。issue 089 に記録)。
printf '\n## Test 7: Command substitution in filename is not executed while listing\n'
TEST_DIR="$TEST_TMP/clip7"
mkdir -p "$TEST_DIR"
# スラッシュを含まない名前にする (ファイル名に / は入れられない)。実行されると
# cwd (= TEST_DIR) に pwned が作られるので、cd 後に作成する。
cd "$TEST_DIR" || exit 1
EVIL_NAME='evil$(touch pwned)file.avi'
echo "dummy video" > "./$EVIL_NAME"
export MOCK_CLIPBOARD="$TEST_DIR/$EVIL_NAME"
# 一覧表示だけを検証対象にするため n で中止する (処理本体の print -P は issue 089 の管轄)
run_av1ify_clip n
assert_file_not_exists "$TEST_DIR/pwned" "Listing does not execute \$(...) in pasted filename (approve path is issue 089)"
assert_contains "$CLIP_OUTPUT" "$EVIL_NAME" "Shows the pasted filename verbatim"

# Test 8: 引数がある呼び出しではクリップボードを読まない
printf '\n## Test 8: Arguments suppress clipboard read\n'
TEST_DIR="$TEST_TMP/clip8"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/arg.avi"
echo "dummy video" > "$TEST_DIR/clip.avi"
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
export MOCK_PBPASTE_FAIL=1
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "クリップボードの読み取りに失敗" "Reports pbpaste failure"
[[ "$CLIP_RC" -eq 1 ]] && printf '✓ Exit code is 1 on pbpaste failure\n' || { printf '✗ Exit code is 1 on pbpaste failure (got %s)\n' "$CLIP_RC"; exit 1; }
unset MOCK_PBPASTE_FAIL

# Test 10: 本番のゲートは「このテスト自身の文脈 (非対話)」で閉じている
# 🚨 ここは seam を差し替えない。差し替えテストは「ゲート関数が偽ならヘルプ」しか pin できず、
#    「スクリプトでは発火しない」という後方互換の主張を一切守らない (red team 指摘)。
printf '\n## Test 10: Real gate is closed in this (non-interactive) context\n'
unfunction __av1ify_clipboard_mode_available
source "$ROOT_DIR/zshlib/_av1ify.zsh"   # 本番のゲートを復元する
unsetopt err_exit
__av1ify_clipboard_mode_available; gate_rc=$?
setopt err_exit
[[ "$gate_rc" -ne 0 ]] && printf '✓ Real gate refuses a non-interactive script\n' \
  || { printf '✗ Real gate fired in a non-interactive script (rc=%s)\n' "$gate_rc"; exit 1; }

# Test 11: 対話シェルでも stdin が端末でなければ閉じている (-t 0 側を本番のまま検証)
printf '\n## Test 11: Real gate is closed when stdin is not a terminal (interactive shell)\n'
unsetopt err_exit
gate_out=$(zsh -i -c "source '$ROOT_DIR/zshlib/_av1ify.zsh'; __av1ify_clipboard_mode_available && print FIRED || print CLOSED" < /dev/null 2>/dev/null | tail -1)
setopt err_exit
[[ "$gate_out" == "CLOSED" ]] && printf '✓ Interactive shell with redirected stdin does not fire\n' \
  || { printf '✗ Interactive shell with redirected stdin: got %s\n' "$gate_out"; exit 1; }

# Test 12: ゲートが -o interactive を要求していることを静的に pin する
# 🚨 静的検査にしている理由: setopt interactive は実行時に変更できず、pty も必要なので
#    「対話 + 端末」の真の組み合わせを自動テストから作れない。条件が消えると
#    「端末から起動したスクリプトで発火する」= 削除つき経路が復活するため、
#    文字列としてでも消えないように留める (挙動確認は pty 手動検証と issue 094)。
printf '\n## Test 12: Gate requires -o interactive (static pin)\n'
gate_src=$(functions __av1ify_clipboard_mode_available)
assert_contains "$gate_src" "-o interactive" "Gate checks -o interactive"
assert_contains "$gate_src" "-t 0" "Gate checks stdin is a terminal"
assert_contains "$gate_src" "pbpaste" "Gate checks pbpaste availability"
__av1ify_clipboard_mode_available() { return 0 }   # 以降のテスト用に再び開く

# Test 13: ディレクトリ行は配下の件数を出す (一覧が「1 行 = 1 ファイル」を装わない)
printf '\n## Test 13: Directory line shows expanded file count\n'
TEST_DIR="$TEST_TMP/clip13"
mkdir -p "$TEST_DIR/tree/nested"
echo "dummy video" > "$TEST_DIR/tree/a.avi"
echo "dummy video" > "$TEST_DIR/tree/b.mkv"
echo "dummy video" > "$TEST_DIR/tree/nested/c.mp4"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/tree"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "[ディレクトリ]" "Marks directory lines"
assert_contains "$CLIP_OUTPUT" "配下の動画 3 件" "Shows how many files the directory expands to"
assert_contains "$CLIP_OUTPUT" "処理するファイル 3件" "Summary counts expanded files, not lines"

# Test 14: symlink は解決後の実体を表示する (削除されるのは実体なので)
printf '\n## Test 14: Symlink is shown as its resolved target\n'
TEST_DIR="$TEST_TMP/clip14"
mkdir -p "$TEST_DIR/archive" "$TEST_DIR/shortcuts"
echo "dummy video" > "$TEST_DIR/archive/master.avi"
ln -sf "$TEST_DIR/archive/master.avi" "$TEST_DIR/shortcuts/master.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/shortcuts/master.avi"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "✓ $TEST_DIR/archive/master.avi" "Lists the resolved target"
assert_contains "$CLIP_OUTPUT" "(貼付: $TEST_DIR/shortcuts/master.avi)" "Also shows what was pasted"

# Test 15: 相対パスは絶対パスで表示・処理する
printf '\n## Test 15: Relative path is normalized to absolute\n'
TEST_DIR="$TEST_TMP/clip15"
mkdir -p "$TEST_DIR/sub"
echo "dummy video" > "$TEST_DIR/sub/rel.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="sub/rel.avi"
run_av1ify_clip y
assert_contains "$CLIP_OUTPUT" "✓ $TEST_DIR/sub/rel.avi" "Shows absolute path"
assert_file_exists "$TEST_DIR/sub/rel-enc.mp4" "Converts the relative path"

# Test 16: 先頭が - のファイル名も絶対化されるので処理される
printf '\n## Test 16: Filename starting with a dash is processed\n'
TEST_DIR="$TEST_TMP/clip16"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR" || exit 1
echo "dummy video" > "./-n.avi"
echo "dummy video" > "./plain.avi"
export MOCK_CLIPBOARD="$TEST_DIR/-n.avi
$TEST_DIR/plain.avi"
run_av1ify_clip y
assert_file_exists "$TEST_DIR/-n-enc.mp4" "Converts a file whose name starts with a dash"

# Test 17: ~ 始まりのパスを展開する
printf '\n## Test 17: Leading tilde is expanded\n'
TEST_DIR="$TEST_TMP/clip17"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR" || exit 1
export HOME="$TEST_DIR"
echo "dummy video" > "$TEST_DIR/tilde.avi"
export MOCK_CLIPBOARD="~/tilde.avi"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "✓ $TEST_DIR/tilde.avi" "Expands ~ to \$HOME"
export HOME="$ORIG_HOME"

# Test 18: 大文字 Y / YES も承認として受ける
printf '\n## Test 18: Uppercase Y and YES are accepted\n'
TEST_DIR="$TEST_TMP/clip18"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/upper.avi"
echo "dummy video" > "$TEST_DIR/upper2.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/upper.avi"
run_av1ify_clip Y
assert_file_exists "$TEST_DIR/upper-enc.mp4" "Accepts uppercase Y"
export MOCK_CLIPBOARD="$TEST_DIR/upper2.avi"
run_av1ify_clip YES
assert_file_exists "$TEST_DIR/upper2-enc.mp4" "Accepts uppercase YES"

# Test 19: 元ファイル削除つきの呼び出しでは警告を出す
printf '\n## Test 19: Warns when originals will be trashed\n'
TEST_DIR="$TEST_TMP/clip19"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/del.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/del.avi"
unsetopt err_exit
del_output=$(av1ify --delete-origin-if-success-and-no-ng 2>&1 <<< "n")
setopt err_exit
assert_contains "$del_output" "元ファイルをゴミ箱へ移します" "Warns about trashing originals"
assert_file_exists "$TEST_DIR/del.avi" "Declining keeps the original"

# Test 20: AV1_* の無効値は一覧・確認より先に落とす
printf '\n## Test 20: Invalid AV1_* fails before the confirmation\n'
TEST_DIR="$TEST_TMP/clip20"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/fps.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/fps.avi"
unsetopt err_exit
fps_output=$(AV1_FPS=abc av1ify 2>&1 <<< "y"); fps_rc=$?
setopt err_exit
assert_contains "$fps_output" "無効なfps指定" "Reports the invalid fps"
assert_not_contains "$fps_output" "これを入力にしますか" "Does not ask before validating"
assert_file_not_exists "$TEST_DIR/fps-enc.mp4" "Does not convert on invalid fps"
[[ "$fps_rc" -eq 1 ]] && printf '✓ Exit code is 1 on invalid fps\n' || { printf '✗ Exit code is 1 on invalid fps (got %s)\n' "$fps_rc"; exit 1; }

# Test 21: 先行入力の drain が配線されていることを静的に pin する
# 🚨 実行時テストにできない理由 (実測 2026-08-22): zsh の `read -t 0` は**端末に対してのみ**
#    「入力が溜まっているか」を答える。パイプでは常に偽を返すため、パイプで drain を呼んでも
#    何も捨てられず、テストは「drain が無い実装」と区別できない (= 書いても vacuous)。
#    pty での実測は取れており、先行入力 "junkline" は
#      drain なし → read が junkline を拾う (= 一覧を見る前に承認が成立する)
#      drain あり → 捨てられて後続の read が待つ
#    となる。ここでは配線が消えないことだけを留める。
printf '\n## Test 21: Typeahead drain is wired into the confirmation (static pin)\n'
confirm_src=$(functions __av1ify_targets_from_clipboard)
assert_contains "$confirm_src" "__av1ify_drain_typeahead" "Confirmation drains typeahead first"
drain_src=$(functions __av1ify_drain_typeahead)
assert_contains "$drain_src" "-t 0" "Drain uses a non-blocking read"

# Test 22: av1c (引数なし) もクリップボードを読み、compact プリセット + 元ファイル削除で処理する
# 対話シェルの av1c は _zshrc のラッパー経由で「関数」として動くのでゲートを通る
# (bin/binav1c 経由は非対話なので通らない = Test 10 と同じ理屈)。
printf '\n## Test 22: av1c reads the clipboard with the compact preset and trashes originals\n'
TEST_DIR="$TEST_TMP/clip22"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/shorthand.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/shorthand.avi"
TRASH_LOG="$TEST_TMP/clip22.trash.log"
: > "$TRASH_LOG"
FFMPEG_LOG="$TEST_TMP/clip22.ffmpeg.log"
: > "$FFMPEG_LOG"
unsetopt err_exit
av1c_output=$(TEST_TRASH_LOG="$TRASH_LOG" TEST_FFMPEG_ARGS_LOG="$FFMPEG_LOG" \
  MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1c 2>&1 <<< "y")
av1c_rc=$?
setopt err_exit
assert_contains "$av1c_output" "クリップボードのテキストから読み取りました" "av1c reads the clipboard when called with no arguments"
assert_contains "$av1c_output" "元ファイルをゴミ箱へ移します" "av1c warns that originals will be trashed"
ffmpeg_args="$(<"$FFMPEG_LOG")"
assert_contains "$ffmpeg_args" "720" "av1c applies the compact resolution (720p)"
assert_contains "$ffmpeg_args" "-r 30" "av1c applies the compact fps (30)"
trash_log_contents="$(<"$TRASH_LOG")"
assert_contains "$trash_log_contents" "$TEST_DIR/shorthand.avi" "av1c trashes the clipboard-sourced original"
assert_file_not_exists "$TEST_DIR/shorthand.avi" "Original is gone after av1c"
[[ "$av1c_rc" -eq 0 ]] && printf '✓ av1c exit code is 0 on approval\n' || { printf '✗ av1c exit code is 0 on approval (got %s)\n' "$av1c_rc"; exit 1; }

# Test 23: av1c も n で中止できる (元ファイルは残る)
printf '\n## Test 23: Declining av1c keeps the original\n'
TEST_DIR="$TEST_TMP/clip23"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/keep.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/keep.avi"
unsetopt err_exit
av1c_no_output=$(av1c 2>&1 <<< "n"); av1c_no_rc=$?
setopt err_exit
assert_contains "$av1c_no_output" "中止しました" "av1c reports cancellation"
assert_file_exists "$TEST_DIR/keep.avi" "Declining av1c keeps the original"
[[ "$av1c_no_rc" -eq 130 ]] && printf '✓ av1c exit code is 130 on cancel\n' || { printf '✗ av1c exit code is 130 on cancel (got %s)\n' "$av1c_no_rc"; exit 1; }

# Test 24: 引数つきの av1c はクリップボードを読まない
printf '\n## Test 24: Arguments suppress the clipboard read for av1c too\n'
TEST_DIR="$TEST_TMP/clip24"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/arg.avi"
echo "dummy video" > "$TEST_DIR/clip.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/clip.avi"
export PBPASTE_LOG="$TEST_DIR/pbpaste.log"
unsetopt err_exit
MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1c "$TEST_DIR/arg.avi" >/dev/null 2>&1
setopt err_exit
assert_file_not_exists "$PBPASTE_LOG" "Does not call pbpaste when av1c gets arguments"
assert_file_exists "$TEST_DIR/clip.avi" "Clipboard path is untouched when av1c gets arguments"
unset PBPASTE_LOG

# Test 25: プリセットのフラグは zshlib/_av1ify.zsh の av1c() だけに書く (bin/binav1c は呼ぶだけ)
# 書き写すと片方だけ変わる。bin/binav1c 側の実行行がフラグを持たないことを静的に pin する。
printf '\n## Test 25: bin/binav1c delegates to av1c() instead of copying the flags (static pin)\n'
av1c_script_code=$(grep -v '^[[:space:]]*#' "$ROOT_DIR/bin/binav1c" | grep -v '^[[:space:]]*$')
assert_contains "$av1c_script_code" 'av1c "$@"' "bin/binav1c calls av1c()"
assert_not_contains "$av1c_script_code" "--compact" "bin/binav1c does not restate the preset flags"
av1c_src=$(functions av1c)
assert_contains "$av1c_src" "--compact" "av1c() holds the compact flag"
assert_contains "$av1c_src" "--delete-origin-if-success-and-no-ng" "av1c() holds the delete-origin flag"

# Test 26: 1 行に空白区切りで複数パスが並んでいたら、分割して対象にする
# (シェルのコマンドラインや、パスを空白で連結するツールからそのままコピーすると起きる。
#  以前は行全体が 1 個のパス名として ✗ になり、別コマンドを案内するだけだった)
printf '\n## Test 26: Space-separated paths on one line are split and accepted\n'
TEST_DIR="$TEST_TMP/clip26"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
echo "dummy video" > "$TEST_DIR/sample b.avi"
echo "dummy video" > "$TEST_DIR/sample_c.avi"
cd "$TEST_DIR" || exit 1
# クォート無し / シングルクォート付きの混在 = zsh のコマンドラインからコピーした形
export MOCK_CLIPBOARD="$TEST_DIR/sample_a.avi '$TEST_DIR/sample b.avi' $TEST_DIR/sample_c.avi"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "1 行に 3 件のパスが並んでいます" "Reports how many paths the line held"
assert_contains "$CLIP_OUTPUT" "[分割] $TEST_DIR/sample_a.avi" "Lists the unquoted path as a split entry"
assert_contains "$CLIP_OUTPUT" "[分割] $TEST_DIR/sample b.avi" "Lists the quoted path with a space"
assert_contains "$CLIP_OUTPUT" "対象 3件 / 除外 0件" "Counts all three as targets"
assert_contains "$CLIP_OUTPUT" "空白区切りとして解釈しました" "Says the line was interpreted as a split"
assert_not_contains "$CLIP_OUTPUT" "見つかりません" "The line itself is no longer reported as missing"

# Test 27: 分割した行が実際に変換まで通る (一覧に出すだけで処理されないと意味がない)
printf '\n## Test 27: The split entries are actually converted after y\n'
TEST_DIR="$TEST_TMP/clip27"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
echo "dummy video" > "$TEST_DIR/sample b.avi"
echo "dummy video" > "$TEST_DIR/sample_c.avi"
cd "$TEST_DIR" || exit 1
# 1 行目は空白区切り (クォート付きを含む)、2 行目は普通の 1 行 1 パス = 最悪ケースの混在
export MOCK_CLIPBOARD="$TEST_DIR/sample_a.avi '$TEST_DIR/sample b.avi'
$TEST_DIR/sample_c.avi"
MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 run_av1ify_clip y
[[ "$CLIP_RC" -eq 0 ]] && printf '✓ Exit code is 0\n' || { printf '✗ Exit code is 0 (got %s)\n' "$CLIP_RC"; exit 1; }
assert_file_exists "$TEST_DIR/sample_a-enc.mp4" "Converts the unquoted path from the split line"
assert_file_exists "$TEST_DIR/sample b-enc.mp4" "Converts the quoted path with a space"
assert_file_exists "$TEST_DIR/sample_c-enc.mp4" "Converts the newline-separated path"

# Test 28: 空白入りの単一パス (実在しない) を分割しない
# 判定を「分割した語が実在するか」に置いているので、ただのタイプミスでは割れない
printf '\n## Test 28: A single missing path with spaces is not split\n'
TEST_DIR="$TEST_TMP/clip28"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/present.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/no such clip.avi
$TEST_DIR/present.avi"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "見つかりません" "Still reports the missing path"
assert_not_contains "$CLIP_OUTPUT" "[分割]" "No split for a plain typo"

# Test 29: 分割語のうち解決できなかったものは ✗ で除外する (黙って消さない)
printf '\n## Test 29: Unresolvable words inside a split line are shown as excluded\n'
TEST_DIR="$TEST_TMP/clip29"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
echo "dummy video" > "$TEST_DIR/sample_b.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/sample_a.avi $TEST_DIR/gone/missing.avi $TEST_DIR/sample_b.avi"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "✗ [分割] $TEST_DIR/gone/missing.avi" "Names the word it could not resolve"
assert_contains "$CLIP_OUTPUT" "対象 2件 / 除外 1件" "Counts the missing word as excluded"

# Test 30: 分割判定でファイル名の $(...) を実行しない
# 判定は (z) の字句解析だけで行う (展開しない)。ここが eval / print -P に変わると
# クリップボードの中身が実行される (issue 089 と同じ穴)
printf '\n## Test 30: Splitting does not execute $(...) in the pasted line\n'
TEST_DIR="$TEST_TMP/clip30"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR" || exit 1
EVIL_NAME='evil$(touch pwned)file.avi'
echo "dummy video" > "./$EVIL_NAME"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
export MOCK_CLIPBOARD="$TEST_DIR/$EVIL_NAME $TEST_DIR/sample_a.avi"
run_av1ify_clip n
assert_file_not_exists "$TEST_DIR/pwned" "Splitting the line does not execute \$(...)"
assert_contains "$CLIP_OUTPUT" "[分割]" "Still splits the line (both words exist)"

# Test 31: cwd にたまたま同名のファイル/ディレクトリがあっても誤って割らない
# (敵対的レビュー 2026-08-23 の指摘。判定が「分割した語が実在するか」だけだと、
#  `src` と `test` がある場所で「src test 用のメモ.avi」という 1 個のファイル名が
#  「複数パス」に化ける。判定にはパス区切りを含む語だけを数える)
printf '\n## Test 31: Bare words that exist in cwd do not cause a split\n'
TEST_DIR="$TEST_TMP/clip31"
mkdir -p "$TEST_DIR/src" "$TEST_DIR/test"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="src test sample notes.avi"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "見つかりません" "Still reports the missing path"
assert_not_contains "$CLIP_OUTPUT" "[分割]" "No split from cwd-relative collisions"

# Test 32: 分割して 1 語しか実在しない行は割らない (「2 件以上」の下限境界)
printf '\n## Test 32: One resolvable word is not enough to split\n'
TEST_DIR="$TEST_TMP/clip32"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/sample_a.avi $TEST_DIR/gone/sample_b.avi"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "見つかりません" "Still reports the missing path"
assert_not_contains "$CLIP_OUTPUT" "[分割]" "No split when only one word resolves"

# Test 33: 先頭 ~ の語も分割して受け取る (resolve が HOME を展開する)
# 以前は「案内する ${(Q)${(z)...}} が ~ を展開しない」ために数えなかったが、
# 案内をやめて自分で処理するようになったので、その制限は無くなった
printf '\n## Test 33: Tilde-prefixed words are split and expanded\n'
TEST_DIR="$TEST_TMP/clip33"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
echo "dummy video" > "$TEST_DIR/sample_b.avi"
cd "$TEST_DIR" || exit 1
HOME="$TEST_DIR"
export MOCK_CLIPBOARD="~/sample_a.avi ~/sample_b.avi"
run_av1ify_clip n
HOME="$ORIG_HOME"
assert_contains "$CLIP_OUTPUT" "[分割] $TEST_DIR/sample_a.avi" "Expands ~ to the real path"
assert_contains "$CLIP_OUTPUT" "対象 2件 / 除外 0件" "Both tilde paths become targets"

# Test 34: GLOB_SUBST / NOMATCH が立っていても、判定は展開せず落ちない
# (実測 2026-08-23: 素の ${(z)line} は GLOB_SUBST 下でグロブ展開され、NOMATCH と
#  重なると未捕捉エラーで呼び出し元のループごと落ちる)
printf '\n## Test 34: Judgement is unaffected by GLOB_SUBST / NOMATCH\n'
TEST_DIR="$TEST_TMP/clip34"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
echo "dummy video" > "$TEST_DIR/sample_b.avi"
cd "$TEST_DIR" || exit 1
setopt glob_subst nomatch
unsetopt err_exit
# 🚨 サブシェルで走らせること。ガードが無いと NOMATCH の未捕捉エラーで
#    「テストスクリプトごと落ちる」= ✗ を一度も出さずに終わるため、変異させても
#    失敗として観測できない (この形で実際に見落とした。2026-08-23)。
#    サブシェルなら死んでも親は続き、出力が空になることで red として出る。
glob_probe=$(__av1ify_split_space_separated_paths "$TEST_DIR/*.avi $TEST_DIR/sample_a.avi" 2>&1; print -r -- "REPLY=$REPLY")
nomatch_probe=$(__av1ify_split_space_separated_paths "$TEST_DIR/no_such_*.avi $TEST_DIR/sample_a.avi" 2>&1; print -r -- "REPLY=$REPLY")
setopt err_exit
unsetopt glob_subst nomatch
[[ "$glob_probe" == "REPLY=1" ]] && printf '✓ Glob pattern is not expanded into several hits\n' || { printf '✗ Glob pattern is not expanded (got: %s)\n' "$glob_probe"; exit 1; }
[[ "$nomatch_probe" == "REPLY=1" ]] && printf '✓ Unmatched glob neither expands nor raises\n' || { printf '✗ Unmatched glob neither expands nor raises (got: %s)\n' "$nomatch_probe"; exit 1; }

# Test 35: 分割関数は呼び出し元の状態を壊さない (契約の白箱検証)
# ブラックボックスでは観測できない (現在の呼び出し元が呼ぶ前に値を控えているため)
printf '\n## Test 35: The split helper does not clobber the caller state\n'
TEST_DIR="$TEST_TMP/clip35"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
echo "dummy video" > "$TEST_DIR/sample_b.avi"
cd "$TEST_DIR" || exit 1
__AV1IFY_PASTED_TRIMMED="sentinel-before-call"
unsetopt err_exit
__av1ify_split_space_separated_paths "$TEST_DIR/sample_a.avi $TEST_DIR/sample_b.avi"
setopt err_exit
[[ "$__AV1IFY_PASTED_TRIMMED" == "sentinel-before-call" ]] && printf '✓ __AV1IFY_PASTED_TRIMMED is restored\n' || { printf '✗ __AV1IFY_PASTED_TRIMMED is restored (got %s)\n' "$__AV1IFY_PASTED_TRIMMED"; exit 1; }

# Test 36: av1c のときは分割したことを警告つきで見せる (ゴミ箱移動まで走るため)
printf '\n## Test 36: av1c still warns that originals go to the trash\n'
TEST_DIR="$TEST_TMP/clip36"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
echo "dummy video" > "$TEST_DIR/sample_b.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/sample_a.avi $TEST_DIR/sample_b.avi"
unsetopt err_exit
warn_output=$(av1c 2>&1 <<< "n")
setopt err_exit
assert_contains "$warn_output" "空白区切りとして解釈しました" "Says the line was split"
assert_contains "$warn_output" "元ファイルをゴミ箱へ移します" "av1c still warns about the trash"

# Test 37: Finder のファイル選択 (ペーストボードの file URL) を読む
# 🚨 Finder の Cmd+C はプレーンテキストを載せないので、pbpaste だけを見ていると
#    「クリップボードが空です」で必ず落ちる (実測 2026-09-04)
printf '\n## Test 37: Finder file selection (pasteboard file URLs) is read\n'
TEST_DIR="$TEST_TMP/clip37"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
echo "dummy video" > "$TEST_DIR/sample b.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD=""
export MOCK_PASTEBOARD_FILES="$TEST_DIR/sample_a.avi
$TEST_DIR/sample b.avi"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "クリップボードのファイル選択から読み取りました (2 件)" "Announces the file-URL flavor"
assert_contains "$CLIP_OUTPUT" "対象 2件 / 除外 0件" "Both selected files become targets"
assert_not_contains "$CLIP_OUTPUT" "クリップボードが空です" "Does not fall back to the empty text flavor"
unset MOCK_PASTEBOARD_FILES

# Test 38: ファイル URL がテキストより優先される (Finder の選択が本体)
printf '\n## Test 38: File URLs win over the plain-text flavor\n'
TEST_DIR="$TEST_TMP/clip38"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/from_url.avi"
echo "dummy video" > "$TEST_DIR/from_text.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/from_text.avi"
export MOCK_PASTEBOARD_FILES="$TEST_DIR/from_url.avi"
run_av1ify_clip n
assert_contains "$CLIP_OUTPUT" "$TEST_DIR/from_url.avi" "Uses the file URL"
assert_not_contains "$CLIP_OUTPUT" "from_text.avi" "Ignores the text flavor when file URLs exist"
unset MOCK_PASTEBOARD_FILES

# Test 39: osascript が失敗したら、その出力を使わずテキスト経路へフォールバックする
# (JXA の例外・将来の権限変更・非 GUI セッションで file URL が取れないことがある。
#  そこで止まると今まで動いていたテキスト貼り付けまで巻き添えで死に、逆に rc を見ずに
#  stdout だけ使うと「途中まで出た一覧」を全件のつもりで処理する)
printf '\n## Test 39: A failing osascript is not trusted and the text flavor is used\n'
TEST_DIR="$TEST_TMP/clip39"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/from_text.avi"
echo "dummy video" > "$TEST_DIR/partial.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/from_text.avi"
export MOCK_PASTEBOARD_FILES="$TEST_DIR/partial.avi"
export MOCK_OSASCRIPT_FAIL=1
run_av1ify_clip n
unset MOCK_OSASCRIPT_FAIL MOCK_PASTEBOARD_FILES
assert_contains "$CLIP_OUTPUT" "クリップボードのテキストから読み取りました" "Falls back to the text flavor"
assert_contains "$CLIP_OUTPUT" "対象 1件 / 除外 0件" "Still resolves the pasted path"
assert_not_contains "$CLIP_OUTPUT" "partial.avi" "Output of the failed osascript run is discarded"

# Test 40: KSH_ARRAYS が立っていても、分割行の一覧と処理対象がずれない
# (敵対的レビュー 2026-09-04 の P1。0-based だと添字ループが先頭を飛ばして末尾で
#  範囲外になり、「1 行に 3 件」と出したのに 2 件 + 空文字を対象にする。
#  旧 __av1ify_invocation_name でも同じ罠を踏んでいる)
printf '\n## Test 40: Split listing survives KSH_ARRAYS\n'
TEST_DIR="$TEST_TMP/clip40"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/sample_a.avi"
echo "dummy video" > "$TEST_DIR/sample_b.avi"
echo "dummy video" > "$TEST_DIR/sample_c.avi"
cd "$TEST_DIR" || exit 1
export MOCK_CLIPBOARD="$TEST_DIR/sample_a.avi $TEST_DIR/sample_b.avi $TEST_DIR/sample_c.avi"
setopt ksh_arrays
run_av1ify_clip n
unsetopt ksh_arrays
assert_contains "$CLIP_OUTPUT" "[分割] $TEST_DIR/sample_a.avi" "Does not drop the first split entry under KSH_ARRAYS"
assert_contains "$CLIP_OUTPUT" "[分割] $TEST_DIR/sample_c.avi" "Does not drop the last split entry under KSH_ARRAYS"
assert_contains "$CLIP_OUTPUT" "対象 3件 / 除外 0件" "Listing and target count agree under KSH_ARRAYS"

printf '\n=== av1ify Clipboard Tests: all passed ===\n'
