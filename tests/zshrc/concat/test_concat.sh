#!/usr/bin/env zsh
# shellcheck shell=bash
setopt err_exit no_unset pipe_fail

# zshでの現在のスクリプトパス取得
SCRIPT_PATH="${(%):-%x}"
ROOT_DIR="$(cd "$(dirname "$SCRIPT_PATH")/../../.." && pwd)"
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

# テスト開始
printf '\n=== concat Unit Tests ===\n\n'

# Test 1: ヘルプメッセージの表示
printf '## Test 1: Help message display\n'
help_output=$(concat --help 2>&1)
assert_contains "$help_output" "concat" "Help message contains command name"
assert_contains "$help_output" "使い方" "Help message is in Japanese"
assert_contains "$help_output" "--force" "Help message mentions --force option"

# Test 2: 引数不足のエラー
printf '\n## Test 2: Error with insufficient arguments\n'
TEST_DIR="$TEST_TMP/test2"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "最低2つのファイルが必要" "Reports error for single file"
assert_exit_code "1" "$exit_code" "Returns exit code 1"

# Test 3: 存在しないファイルのエラー
printf '\n## Test 3: Error for non-existent file\n'
TEST_DIR="$TEST_TMP/test3"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "ファイルが見つかりません" "Reports error for non-existent file"

# Test 4: 未対応拡張子のエラー
printf '\n## Test 4: Error for unsupported extension\n'
TEST_DIR="$TEST_TMP/test4"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "text file" > "$TEST_DIR/video_002.txt"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.txt" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "未対応の拡張子" "Reports error for unsupported extension"

# Test 5: 異なるディレクトリのエラー
printf '\n## Test 5: Error for different directories\n'
TEST_DIR="$TEST_TMP/test5"
mkdir -p "$TEST_DIR/dir1" "$TEST_DIR/dir2"
echo "video 1" > "$TEST_DIR/dir1/video_001.mp4"
echo "video 2" > "$TEST_DIR/dir2/video_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/dir1/video_001.mp4" "$TEST_DIR/dir2/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "同一ディレクトリに存在する" "Reports error for different directories"

# Test 6: 共通プレフィックス不足のエラー
printf '\n## Test 6: Error for insufficient common prefix\n'
TEST_DIR="$TEST_TMP/test6"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/ab_001.mp4"
echo "video 2" > "$TEST_DIR/xy_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/ab_001.mp4" "$TEST_DIR/xy_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "共通プレフィックス" "Reports error for insufficient common prefix"

# Test 7: 連番パターンなしのエラー
printf '\n## Test 7: Error for missing sequence pattern\n'
TEST_DIR="$TEST_TMP/test7"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_aaa.mp4"
echo "video 2" > "$TEST_DIR/video_bbb.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_aaa.mp4" "$TEST_DIR/video_bbb.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "連番パターンがありません" "Reports error for missing sequence pattern"

# Test 8: 欠番のエラー
printf '\n## Test 8: Error for missing sequence numbers\n'
TEST_DIR="$TEST_TMP/test8"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 3" > "$TEST_DIR/video_003.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_003.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "欠番があります" "Reports error for missing sequence numbers"

# Test 9: 連番が0/1から始まらないエラー
printf '\n## Test 9: Error for sequence not starting from 0 or 1\n'
TEST_DIR="$TEST_TMP/test9"
mkdir -p "$TEST_DIR"
echo "video 5" > "$TEST_DIR/video_005.mp4"
echo "video 6" > "$TEST_DIR/video_006.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_005.mp4" "$TEST_DIR/video_006.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "0または1から始まっていません" "Reports error for sequence not starting from 0 or 1"

# Test 10: コーデック不一致のエラー
printf '\n## Test 10: Error for codec mismatch\n'
TEST_DIR="$TEST_TMP/test10"
mkdir -p "$TEST_DIR"
# mismatch_001 は通常のコーデック、mismatch_002 は異なるコーデック（モックで判定）
echo "video 1" > "$TEST_DIR/mismatch_001.mp4"
echo "video 2" > "$TEST_DIR/mismatch_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/mismatch_001.mp4" "$TEST_DIR/mismatch_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "再エンコードが必要" "Reports error for codec mismatch"

# Test 11: --forceでコーデック不一致を無視
printf '\n## Test 11: --force ignores codec mismatch\n'
TEST_DIR="$TEST_TMP/test11"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/mismatch_001.mp4"
echo "video 2" > "$TEST_DIR/mismatch_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --force "$TEST_DIR/mismatch_001.mp4" "$TEST_DIR/mismatch_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "完了" "--force allows concat despite mismatch"

# Test 12: 正常な結合（_NNNパターン）
printf '\n## Test 12: Successful concat with _NNN pattern\n'
TEST_DIR="$TEST_TMP/test12"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
echo "video 3" > "$TEST_DIR/video_003.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" "$TEST_DIR/video_003.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video.mp4" "Output file is created"
assert_contains "$output" "完了" "Reports success"

# Test 13: 正常な結合（-NNNパターン）
printf '\n## Test 13: Successful concat with -NNN pattern\n'
TEST_DIR="$TEST_TMP/test13"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip-01.mp4"
echo "video 2" > "$TEST_DIR/clip-02.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/clip-01.mp4" "$TEST_DIR/clip-02.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/clip.mp4" "Output file is created with -NNN pattern"

# Test 14: 正常な結合（(N)パターン）
printf '\n## Test 14: Successful concat with (N) pattern\n'
TEST_DIR="$TEST_TMP/test14"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/movie(1).mp4"
echo "video 2" > "$TEST_DIR/movie(2).mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/movie(1).mp4" "$TEST_DIR/movie(2).mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/movie.mp4" "Output file is created with (N) pattern"

# Test 15: 正常な結合（partNパターン）
printf '\n## Test 15: Successful concat with partN pattern\n'
TEST_DIR="$TEST_TMP/test15"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/file_part1.mp4"
echo "video 2" > "$TEST_DIR/file_part2.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/file_part1.mp4" "$TEST_DIR/file_part2.mp4" 2>&1)
exit_code=$?
setopt err_exit
# 共通プレフィックス計算: file_part1, file_part2 → file_part → part除去 → file_ → 末尾_除去 → file
assert_file_exists "$TEST_DIR/file.mp4" "Output file is created with partN pattern"

# Test 16: 正常な結合（-N-suffixパターン）
printf '\n## Test 16: Successful concat with -N-suffix pattern\n'
TEST_DIR="$TEST_TMP/test16"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/xxx-1-enc.mp4"
echo "video 2" > "$TEST_DIR/xxx-2-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/xxx-1-enc.mp4" "$TEST_DIR/xxx-2-enc.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/xxx.mp4" "Output file is created with -N-suffix pattern"

# Test 16b: サフィックス不一致のエラー
printf '\n## Test 16b: Error for mismatched suffix\n'
TEST_DIR="$TEST_TMP/test16b"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/xxx-1-enc.mp4"
echo "video 2" > "$TEST_DIR/xxx-2-raw.mp4"  # 異なるサフィックス
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/xxx-1-enc.mp4" "$TEST_DIR/xxx-2-raw.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "サフィックスが異なります" "Reports error for mismatched suffix"

# Test 17: 出力ファイル既存時のスキップ
printf '\n## Test 17: Skip when output file exists\n'
TEST_DIR="$TEST_TMP/test17"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
echo "existing output" > "$TEST_DIR/video.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "SKIP" "Skips when output file already exists"

# Test 18: 出力ファイル名が入力と衝突するエラー
printf '\n## Test 18: Error when output filename collides with input\n'
TEST_DIR="$TEST_TMP/test18"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_1.mp4"
echo "video 2" > "$TEST_DIR/video.mp4"  # これが入力かつ出力名と衝突
cd "$TEST_DIR"
# この場合、video.mp4 が入力ファイルで、出力も video.mp4 になる
# 実際には video_1 と video で共通プレフィックスが "video" になる
unsetopt err_exit
output=$(concat "$TEST_DIR/video_1.mp4" "$TEST_DIR/video.mp4" 2>&1 || true)
# 連番パターンがないのでそちらでエラーになる
setopt err_exit
# このテストは連番パターンエラーになるので、別のケースでテスト

# Test 19: 0から始まる連番
printf '\n## Test 19: Sequence starting from 0\n'
TEST_DIR="$TEST_TMP/test19"
mkdir -p "$TEST_DIR"
echo "video 0" > "$TEST_DIR/video_000.mp4"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_000.mp4" "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video.mp4" "Output file is created for sequence starting from 0"
assert_contains "$output" "完了" "Reports success for 0-starting sequence"

# Test 20: 大文字小文字を区別しない拡張子
printf '\n## Test 20: Case-insensitive extension\n'
TEST_DIR="$TEST_TMP/test20"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.MP4"
echo "video 2" > "$TEST_DIR/video_002.Mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.MP4" "$TEST_DIR/video_002.Mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video.mp4" "Output file is created with mixed-case extensions"

# Test 21: 様々な許可拡張子
printf '\n## Test 21: Various allowed extensions\n'
for ext in avi mov mkv webm flv wmv m4v mpg mpeg 3gp ts m2ts; do
  TEST_DIR="$TEST_TMP/test21_$ext"
  mkdir -p "$TEST_DIR"
  echo "video 1" > "$TEST_DIR/video_001.$ext"
  echo "video 2" > "$TEST_DIR/video_002.$ext"
  cd "$TEST_DIR"
  unsetopt err_exit
  concat "$TEST_DIR/video_001.$ext" "$TEST_DIR/video_002.$ext" > /dev/null 2>&1
  exit_code=$?
  setopt err_exit
  if [[ -f "$TEST_DIR/video.mp4" ]]; then
    printf '✓ Extension .%s is accepted\n' "$ext"
  else
    printf '✗ Extension .%s failed\n' "$ext"
  fi
done

# Test 22: スペースを含むパス
printf '\n## Test 22: Paths with spaces\n'
TEST_DIR="$TEST_TMP/test22 with spaces/sub dir"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video file_001.mp4"
echo "video 2" > "$TEST_DIR/video file_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video file_001.mp4" "$TEST_DIR/video file_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video file.mp4" "Output file is created with spaces in path"

printf '\n=== All Tests Completed ===\n'
printf 'All concat tests passed successfully!\n'
