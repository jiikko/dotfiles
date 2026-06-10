#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify テスト共通ヘルパー
# 各テストファイルの冒頭で source して使う

setopt err_exit no_unset pipe_fail

# zshでの現在のスクリプトパス取得（呼び出し元のパスを使う）
_HELPER_DIR="${0:A:h}"
ROOT_DIR="$(cd "$_HELPER_DIR/../../.." && pwd)"
TEST_TMP="$(mktemp -d)"

cleanup() {
  rm -rf "$TEST_TMP"
}
trap cleanup EXIT

# モックスクリプト用のディレクトリ
MOCK_BIN_DIR="$TEST_TMP/mock_bin"
mkdir -p "$MOCK_BIN_DIR"

# ffmpegモックスクリプトを作成（シンプル版）
cat > "$MOCK_BIN_DIR/ffmpeg" <<'EOF'
#!/usr/bin/env sh
# -h フラグ（encoder チェック等）は即 exit 0
for arg in "$@"; do
  case "$arg" in -h) exit 0 ;; esac
done

# 最後の引数を出力ファイルとして扱う
for arg in "$@"; do
  last_arg="$arg"
done

# -で始まらない最後の引数が出力ファイル
if [ -n "$last_arg" ] && [ "${last_arg#-}" = "$last_arg" ]; then
  if [ -n "$MOCK_FFMPEG_OUTPUT_SIZE" ]; then
    dd if=/dev/zero of="$last_arg" bs=1 count="$MOCK_FFMPEG_OUTPUT_SIZE" 2>/dev/null
  else
    echo "mock" > "$last_arg"
  fi
  exit 0
fi
exit 1
EOF
chmod +x "$MOCK_BIN_DIR/ffmpeg"

# ffprobeモックスクリプトを作成（ソース vs 出力ファイルを区別）
cat > "$MOCK_BIN_DIR/ffprobe" <<'EOF'
#!/usr/bin/env sh
# どんなクエリでも成功を返す（MOCK_WIDTH/MOCK_HEIGHT で上書き可能）
# -enc を含むファイルは出力ファイル扱い（MOCK_OUTPUT_* で上書き可能）
if echo "$*" | grep -q "codec_name"; then
  if echo "$*" | grep -q "select_streams v"; then
    last_arg=""
    for arg in "$@"; do last_arg="$arg"; done
    case "$last_arg" in
      *-enc*|*check_ng*) echo "${MOCK_OUTPUT_VCODEC-av1}" ;;
      *) echo "${MOCK_VCODEC-h264}" ;;
    esac
  else
    echo "${MOCK_ACODEC-aac}"
  fi
elif echo "$*" | grep -q "stream=index"; then
  # 音声ストリーム有無の判定用。「音声なし」を再現するには MOCK_AUDIO_INDEX= (空) を設定
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_AUDIO_INDEX-0}" ;;
    *) echo "${MOCK_AUDIO_INDEX-0}" ;;
  esac
elif echo "$*" | grep -q "sample_rate"; then
  echo "${MOCK_SAMPLE_RATE-48000}"
elif echo "$*" | grep -q "stream=channels"; then
  echo "${MOCK_CHANNELS-2}"
elif echo "$*" | grep -q "stream=bit_rate"; then
  echo "${MOCK_AUDIO_BITRATE-248000}"
elif echo "$*" | grep -q "nb_frames"; then
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_NB_FRAMES-${MOCK_NB_FRAMES-300}}" ;;
    *) echo "${MOCK_NB_FRAMES-300}" ;;
  esac
elif echo "$*" | grep -q "format=duration"; then
  # format duration: ソース vs 出力を区別（-enc を含むファイルは出力扱い）
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_FORMAT_DURATION-${MOCK_FORMAT_DURATION-10.0}}" ;;
    *) echo "${MOCK_FORMAT_DURATION-10.0}" ;;
  esac
elif echo "$*" | grep -q "select_streams v:0" && echo "$*" | grep -q "duration"; then
  # ソース vs 出力で別の値を返せるよう -enc を含むファイルは出力扱い
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_VIDEO_DURATION-${MOCK_VIDEO_DURATION-10.0}}" ;;
    *) echo "${MOCK_VIDEO_DURATION-10.0}" ;;
  esac
elif echo "$*" | grep -q "select_streams a:0" && echo "$*" | grep -q "duration"; then
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_AUDIO_DURATION-${MOCK_AUDIO_DURATION-10.0}}" ;;
    *) echo "${MOCK_AUDIO_DURATION-10.0}" ;;
  esac
elif echo "$*" | grep -q "packet=pts_time"; then
  # __av1ify_get_stream_end フォールバック用: 末尾 PTS だけ返す
  # 本物の ffprobe は何百万行も出すが、テストでは tail 相当の最後 1 行で十分
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  if echo "$*" | grep -q "select_streams v"; then
    case "$last_arg" in
      *-enc*|*check_ng*) echo "${MOCK_OUTPUT_VIDEO_LAST_PTS-${MOCK_VIDEO_LAST_PTS-${MOCK_VIDEO_DURATION-10.0}}}" ;;
      *) echo "${MOCK_VIDEO_LAST_PTS-${MOCK_VIDEO_DURATION-10.0}}" ;;
    esac
  elif echo "$*" | grep -q "select_streams a"; then
    case "$last_arg" in
      *-enc*|*check_ng*) echo "${MOCK_OUTPUT_AUDIO_LAST_PTS-${MOCK_AUDIO_LAST_PTS-${MOCK_AUDIO_DURATION-10.0}}}" ;;
      *) echo "${MOCK_AUDIO_LAST_PTS-${MOCK_AUDIO_DURATION-10.0}}" ;;
    esac
  fi
elif echo "$*" | grep -q "packet=dts"; then
  # DTS単調性チェック用 (__video_health_check チェック3): MOCK_DTS_BACKWARD=1 で逆行を注入
  if [ -n "${MOCK_DTS_BACKWARD-}" ]; then
    printf '0\n3750\n2000\n7500\n'   # 3750→2000 で逆行=破損
  else
    printf '0\n3750\n7500\n11250\n'  # 単調増加=健全
  fi
elif echo "$*" | grep -q "duration"; then
  echo "10.0"
elif echo "$*" | grep -q "avg_frame_rate"; then
  echo "${MOCK_AVG_FPS-${MOCK_FPS-30000/1001}}"
elif echo "$*" | grep -q "r_frame_rate"; then
  echo "${MOCK_FPS-30000/1001}"
elif echo "$*" | grep -q "stream_side_data=rotation"; then
  echo "${MOCK_ROTATION-}"
elif echo "$*" | grep -q "width"; then
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_WIDTH-${MOCK_WIDTH-1920}}" ;;
    *) echo "${MOCK_WIDTH-1920}" ;;
  esac
elif echo "$*" | grep -q "height"; then
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_HEIGHT-${MOCK_HEIGHT-1080}}" ;;
    *) echo "${MOCK_HEIGHT-1080}" ;;
  esac
elif echo "$*" | grep -q "format_name"; then
  echo "mp4"
fi
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffprobe"

# trash モック: 受け取った引数を TEST_TRASH_LOG に追記し、対象ファイルを削除する
# (本物の /usr/bin/trash と違い、Finder の Trash には触らない)
cat > "$MOCK_BIN_DIR/trash" <<'EOF'
#!/usr/bin/env sh
log_file="${TEST_TRASH_LOG:-/dev/null}"
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$log_file"
  [ -e "$arg" ] && rm -f -- "$arg"
done
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/trash"

# mount モック: MOCK_MOUNT_OUTPUT があればそれを出力、無ければ実際の mount に委譲
# `mount` 出力の典型行: "device on /mountpoint (fstype, opts...)"
cat > "$MOCK_BIN_DIR/mount" <<'EOF'
#!/usr/bin/env sh
if [ -n "${MOCK_MOUNT_OUTPUT-}" ]; then
  printf '%s\n' "$MOCK_MOUNT_OUTPUT"
  exit 0
fi
exec /sbin/mount "$@"
EOF
chmod +x "$MOCK_BIN_DIR/mount"

# PATH設定
export PATH="$MOCK_BIN_DIR:$PATH"

# av1ifyをロード
source "$ROOT_DIR/zshlib/_av1ify.zsh"

assert_file_exists() {
  local file="$1"
  local message="$2"
  if [[ -f "$file" ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (file not found: %s)\n' "$message" "$file"
    return 1
  fi
}

assert_file_not_exists() {
  local file="$1"
  local message="$2"
  if [[ ! -f "$file" ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (file exists: %s)\n' "$message" "$file"
    return 1
  fi
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (expected to contain: %s)\n' "$message" "$needle"
    return 1
  fi
}

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (expected NOT to contain: %s)\n' "$message" "$needle"
    return 1
  fi
}
