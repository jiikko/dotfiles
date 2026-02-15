#!/usr/bin/env zsh
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
# 最後の引数を出力ファイルとして扱う
for arg in "$@"; do
  last_arg="$arg"
done

# -で始まらない最後の引数が出力ファイル
if [ -n "$last_arg" ] && [ "${last_arg#-}" = "$last_arg" ]; then
  echo "mock video data" > "$last_arg"
  exit 0
fi
exit 1
EOF
chmod +x "$MOCK_BIN_DIR/ffmpeg"

# ffprobeモックスクリプトを作成（シンプル版）
cat > "$MOCK_BIN_DIR/ffprobe" <<'EOF'
#!/usr/bin/env sh
# どんなクエリでも成功を返す（MOCK_WIDTH/MOCK_HEIGHT で上書き可能）
if echo "$*" | grep -q "codec_name"; then
  echo "aac"
elif echo "$*" | grep -q "stream=index"; then
  echo "0"
elif echo "$*" | grep -q "stream=bit_rate"; then
  echo "${MOCK_AUDIO_BITRATE-248000}"
elif echo "$*" | grep -q "duration"; then
  echo "10.0"
elif echo "$*" | grep -q "r_frame_rate"; then
  echo "${MOCK_FPS-30000/1001}"
elif echo "$*" | grep -q "width"; then
  echo "${MOCK_WIDTH-1920}"
elif echo "$*" | grep -q "height"; then
  echo "${MOCK_HEIGHT-1080}"
elif echo "$*" | grep -q "format_name"; then
  echo "mp4"
fi
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffprobe"

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
