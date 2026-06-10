#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# validate-mp4 最低限の回帰テスト
#
# inner __validate_mp4_check の契約 (return 0/1 + REPLY=理由) と
# outer validate_mp4 の --mark リネームを、専用モック ffprobe/ffmpeg で検証する。
# (av1ify の test_helper は av1ify 向けモックなので流用せず、ここは自前モック)

setopt err_exit no_unset pipe_fail

_HELPER_DIR="${0:A:h}"
ROOT_DIR="$(cd "$_HELPER_DIR/../../.." && pwd)"
TEST_TMP="$(mktemp -d)"
cleanup() { rm -rf "$TEST_TMP"; }
trap cleanup EXIT

# --- モック: ffprobe / ffmpeg を環境変数で制御する ---
MOCK_BIN_DIR="$TEST_TMP/mock_bin"
mkdir -p "$MOCK_BIN_DIR"

# ffprobe モック:
#   MOCK_DURATION       format=duration の戻り (未設定=10.0 / 空文字=unreadable 再現)
#   MOCK_VCODEC         映像 codec_type   (未設定=video)
#   MOCK_ACODEC_TYPE    音声 codec_type   (未設定=audio)
#   MOCK_ACODEC_NAMES   音声 codec_name(改行区切り可) (未設定=aac)
cat > "$MOCK_BIN_DIR/ffprobe" <<'EOF'
#!/usr/bin/env sh
args="$*"
if echo "$args" | grep -q "codec_name"; then
  printf '%s\n' "${MOCK_ACODEC_NAMES-aac}"
elif echo "$args" | grep -q "codec_type"; then
  if echo "$args" | grep -q "select_streams v"; then
    printf '%s\n' "${MOCK_VCODEC-video}"
  else
    printf '%s\n' "${MOCK_ACODEC_TYPE-audio}"
  fi
elif echo "$args" | grep -q "format=duration"; then
  printf '%s\n' "${MOCK_DURATION-10.0}"
fi
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffprobe"

# ffmpeg モック (フルデコードログを stderr に出す):
#   MOCK_DECODE_LOG  デコードログ全文 (未設定=clean な time=00:00:10.00)
cat > "$MOCK_BIN_DIR/ffmpeg" <<'EOF'
#!/usr/bin/env sh
if [ -n "${MOCK_DECODE_LOG-}" ]; then
  printf '%s\n' "$MOCK_DECODE_LOG" >&2
else
  printf 'frame=  240 fps=0.0 q=-0.0 size=N/A time=00:00:10.00 bitrate=N/A speed=20x\n' >&2
fi
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffmpeg"

export PATH="$MOCK_BIN_DIR:$PATH"
source "$ROOT_DIR/zshlib/_validate_mp4.zsh"

# 各 assert は失敗時に return 1 → (err_exit により) スクリプト全体が非ゼロ終了する。
# したがって最後まで到達し exit 0 なら全 assert が通ったことを意味する。
assert_check() {  # <file> <expected_rc> <expected_reason> <message>
  local file="$1" want_rc="$2" want_reason="$3" msg="$4"
  local rc
  # err_exit 下で inner の意図的な非ゼロ return が abort を誘発しないよう保護して捕捉
  __validate_mp4_check "$file" && rc=0 || rc=$?
  if [[ "$rc" == "$want_rc" && "$REPLY" == "$want_reason" ]]; then
    printf '✓ %s\n' "$msg"
  else
    printf '✗ %s (rc=%s reason=%q / want rc=%s reason=%q)\n' "$msg" "$rc" "$REPLY" "$want_rc" "$want_reason"
    return 1
  fi
}
assert_eq() {  # <actual> <expected> <message>
  if [[ "$1" == "$2" ]]; then
    printf '✓ %s\n' "$3"
  else
    printf '✗ %s (got=%q want=%q)\n' "$3" "$1" "$2"; return 1
  fi
}

printf '\n=== validate-mp4 Tests ===\n\n'
touch "$TEST_TMP/x.mp4"
F="$TEST_TMP/x.mp4"

printf '## inner __validate_mp4_check の契約\n'
( export MOCK_VCODEC=video MOCK_ACODEC_TYPE=audio MOCK_ACODEC_NAMES=aac; assert_check "$F" 0 ""             "正常: rc=0 / REPLY空" )
( export MOCK_DURATION="";                                              assert_check "$F" 1 "unreadable"      "durationなし → unreadable" )
( export MOCK_VCODEC="";                                                assert_check "$F" 1 "no-video"        "映像なし → no-video" )
( export MOCK_ACODEC_TYPE="";                                           assert_check "$F" 1 "no-audio"        "音声なし → no-audio" )
( export MOCK_DECODE_LOG="frame=10 time=00:00:01.00
[h264 @ 0x0] corrupt decoded frame";                                    assert_check "$F" 1 "decode-error"    "corrupt検出 → decode-error" )
( export MOCK_DURATION="100.0";                                         assert_check "$F" 1 "truncated"       "宣言100s/実10s → truncated" )
# directional: 実デコード終端が宣言より長くても truncation ではない (元の絶対差バグの回帰ガード)
( export MOCK_DURATION="1.0";                                           assert_check "$F" 0 ""                "宣言1s/実10s (actual>declared) は truncated にしない" )
( export MOCK_ACODEC_NAMES="mp3";                                       assert_check "$F" 1 "trim-drops-audio" "非AAC音声 → trim-drops-audio" )
( export MOCK_ACODEC_NAMES="aac
mp3";                                                                   assert_check "$F" 1 "trim-drops-audio" "複数音声に非AAC混在 → trim-drops-audio" )

printf '\n## outer validate_mp4: --mark リネーム\n'
# 破損 (unreadable) を --mark すると <name>.broken(unreadable).mp4 にリネームされる
BROKEN="$TEST_TMP/bad.mp4"; touch "$BROKEN"
( export MOCK_DURATION=""; validate_mp4 --mark "$BROKEN" >/dev/null 2>&1 || true )
assert_eq "$([[ -f "$TEST_TMP/bad.broken(unreadable).mp4" ]] && echo yes || echo no)" "yes" \
  "破損は .broken(unreadable).mp4 にリネーム"
assert_eq "$([[ -f "$BROKEN" ]] && echo yes || echo no)" "no" \
  "元の bad.mp4 は残らない (mv された)"

# 正常ファイルは --mark でもリネームしない
OKF="$TEST_TMP/good.mp4"; touch "$OKF"
( validate_mp4 --mark "$OKF" >/dev/null 2>&1 || true )
assert_eq "$([[ -f "$OKF" ]] && echo yes || echo no)" "yes" \
  "正常ファイルは --mark でもリネームしない"

# .mp4 以外は SKIP (検証しない)
TXT="$TEST_TMP/note.txt"; touch "$TXT"
out=$(validate_mp4 "$TXT" 2>&1 || true)
assert_eq "$([[ "$out" == *SKIP* ]] && echo yes || echo no)" "yes" \
  ".mp4以外は SKIP"

printf '\n=== validate-mp4 Tests Completed (all asserts passed) ===\n'
