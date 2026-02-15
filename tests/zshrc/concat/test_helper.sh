#!/usr/bin/env zsh
# shellcheck shell=bash
# concat テスト共通ヘルパー
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

# ffmpegモックスクリプトを作成
cat > "$MOCK_BIN_DIR/ffmpeg" <<'FFMPEG_MOCK'
#!/usr/bin/env sh
# concat用のffmpegモック

# 引数をパース
output_file=""
input_file=""
for arg in "$@"; do
  case "$arg" in
    -*)
      ;;
    *)
      # 最後の非オプション引数を出力ファイルとして扱う
      if [ -n "$input_file" ]; then
        output_file="$arg"
      else
        input_file="$arg"
      fi
      ;;
  esac
done

# -f null の場合はデコードテスト（何も出力しない）
if echo "$*" | grep -q "\-f null"; then
  exit 0
fi

# 出力ファイルがあればダミーデータを書き込む
if [ -n "$output_file" ] && [ "${output_file#-}" = "$output_file" ]; then
  echo "mock concatenated video data" > "$output_file"
  exit 0
fi

exit 0
FFMPEG_MOCK
chmod +x "$MOCK_BIN_DIR/ffmpeg"

# ffprobeモックスクリプトを作成
cat > "$MOCK_BIN_DIR/ffprobe" <<'FFPROBE_MOCK'
#!/usr/bin/env sh
# concat用のffprobeモック

# 引数から入力ファイルを特定
input_file=""
for arg in "$@"; do
  case "$arg" in
    -*)
      ;;
    *)
      if [ -f "$arg" ]; then
        input_file="$arg"
      fi
      ;;
  esac
done

# JSON出力（診断用）- 最初にチェック
case "$*" in
  *"-of json"*)
    # 出力ファイル（uuid含む）の場合は合計durationに近い値を返す
    printf '{"format":{"duration":"20.0","bit_rate":"1000000"},"streams":[{"codec_type":"video","codec_name":"h264"},{"codec_type":"audio","codec_name":"aac"}]}\n'
    exit 0
    ;;
esac

# 映像情報
if echo "$*" | grep -q "select_streams v:0"; then
  # mismatch_002 ファイルの場合は異なる情報を返す
  if echo "$input_file" | grep -q "mismatch_002"; then
    echo "hevc,1280,720,30/1,yuv422p"
  else
    echo "h264,1920,1080,30/1,yuv420p"
  fi
  exit 0
fi

# 音声情報
if echo "$*" | grep -q "select_streams a:0"; then
  if echo "$input_file" | grep -q "mismatch_002"; then
    echo "mp3,44100,1"
  else
    echo "aac,48000,2"
  fi
  exit 0
fi

# duration (simple format)
if echo "$*" | grep -q "format=duration"; then
  # 入力ファイルは連番パターン（_001, _002, -01, (1), part1など）を含む
  # 出力ファイルは連番パターンを含まない
  if echo "$input_file" | grep -qE '_[0-9]+\.|_[0-9]+$|-[0-9]+\.|\([0-9]+\)|part[0-9]+'; then
    echo "10.0"
  else
    # 出力ファイル（連番パターンなし）の場合は合計duration
    echo "20.0"
  fi
  exit 0
fi

exit 0
FFPROBE_MOCK
chmod +x "$MOCK_BIN_DIR/ffprobe"

# uuidgenモック
cat > "$MOCK_BIN_DIR/uuidgen" <<'EOF'
#!/usr/bin/env sh
echo "test-uuid-12345"
EOF
chmod +x "$MOCK_BIN_DIR/uuidgen"

# PATH設定
export PATH="$MOCK_BIN_DIR:$PATH"

# テスト用のduration許容誤差を大きく設定（モック環境用）
export CONCAT_DURATION_TOLERANCE=100

# concatをロード
source "$ROOT_DIR/zshlib/_concat.zsh"

# テストヘルパー関数
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
    printf '✗ %s (expected to contain: %s, got: %s)\n' "$message" "$needle" "$haystack"
    return 1
  fi
}

assert_exit_code() {
  local expected="$1"
  local actual="$2"
  local message="$3"
  if [[ "$expected" == "$actual" ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (expected exit code: %s, got: %s)\n' "$message" "$expected" "$actual"
    return 1
  fi
}
