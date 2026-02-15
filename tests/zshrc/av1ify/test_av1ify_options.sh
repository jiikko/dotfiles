#!/usr/bin/env zsh
# shellcheck shell=bash
# av1ify オプションテスト (Test 13-64)
# 解像度、fps、denoise、compact、バリデーション、アップスケール防止、縦長動画、fpsキャップ、音声処理、部分一致

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Options Tests (13-64) ===\n\n'

# Test 13: -f オプションのヘルプメッセージ
printf '## Test 13: Help message includes -f option\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "-f" "Help message contains -f option"
assert_contains "$help_output" "ファイルリスト" "Help message describes file list feature"

# Test 14: --resolution オプションのヘルプメッセージ
printf '\n## Test 14: Help message includes --resolution option\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "--resolution" "Help message contains --resolution option"
assert_contains "$help_output" "-r," "Help message contains -r short option"
assert_contains "$help_output" "アスペクト比は維持" "Help message mentions aspect ratio preservation"

# Test 15: --fps オプションのヘルプメッセージ
printf '\n## Test 15: Help message includes --fps option\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "--fps" "Help message contains --fps option"
assert_contains "$help_output" "フレームレート" "Help message mentions frame rate"

# Test 16: --resolution オプション (dry-run)
printf '\n## Test 16: Resolution option with dry-run\n'
TEST_DIR="$TEST_TMP/test16"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 720p "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=720p" "Dry-run shows resolution=720p"
assert_file_not_exists "$TEST_DIR/input-720p-enc.mp4" "Dry-run does not create output file"

# Test 17: --fps オプション (dry-run)
printf '\n## Test 17: FPS option with dry-run\n'
TEST_DIR="$TEST_TMP/test17"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --fps 24 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "fps=24" "Dry-run shows fps=24"

# Test 18: --resolution と --fps の組み合わせ (dry-run)
printf '\n## Test 18: Resolution and FPS combined with dry-run\n'
TEST_DIR="$TEST_TMP/test18"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 1080p --fps 30 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=1080p" "Dry-run shows resolution=1080p"
assert_contains "$output" "fps=30" "Dry-run shows fps=30"

# Test 19: 無効な解像度のバリデーション（エラー終了）
printf '\n## Test 19: Invalid resolution validation (error exit)\n'
TEST_DIR="$TEST_TMP/test19"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"

unsetopt err_exit
output=$(av1ify --dry-run -r 0 "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "無効な解像度" "Reports invalid resolution for 0"
(( exit_code != 0 )) && printf '✓ Exit code is non-zero for -r 0 (%d)\n' "$exit_code" || printf '✗ Exit code should be non-zero for -r 0 (got %d)\n' "$exit_code"

unsetopt err_exit
output=$(av1ify --dry-run -r 10000 "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "無効な解像度" "Reports invalid resolution for 10000"
(( exit_code != 0 )) && printf '✓ Exit code is non-zero for -r 10000 (%d)\n' "$exit_code" || printf '✗ Exit code should be non-zero for -r 10000 (got %d)\n' "$exit_code"

unsetopt err_exit
output=$(av1ify --dry-run -r abc "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "無効な解像度" "Reports invalid resolution for non-numeric"
(( exit_code != 0 )) && printf '✓ Exit code is non-zero for -r abc (%d)\n' "$exit_code" || printf '✗ Exit code should be non-zero for -r abc (got %d)\n' "$exit_code"

# Test 20: 無効なfpsのバリデーション
printf '\n## Test 20: Invalid FPS validation\n'
TEST_DIR="$TEST_TMP/test20"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --fps 0 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効なfps指定" "Reports invalid FPS for 0"
assert_contains "$output" "fps=auto" "Falls back to auto when invalid"

output=$(av1ify --dry-run --fps 300 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効なfps指定" "Reports invalid FPS for 300"

output=$(av1ify --dry-run --fps abc "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効なfps指定" "Reports invalid FPS for non-numeric"

# Test 21: 有効な解像度のバリエーション
printf '\n## Test 21: Valid resolution variations\n'
TEST_DIR="$TEST_TMP/test21"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"

for res in 480p 720p 1080p 1440p 4k 540; do
  output=$(av1ify --dry-run -r "$res" "$TEST_DIR/input.avi" 2>&1 || true)
  if [[ "$output" != *"無効な解像度"* ]]; then
    printf '✓ Resolution %s is valid\n' "$res"
  else
    printf '✗ Resolution %s should be valid\n' "$res"
  fi
done

# Test 22: 有効なfpsのバリエーション (小数点含む)
printf '\n## Test 22: Valid FPS variations including decimal\n'
TEST_DIR="$TEST_TMP/test22"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"

for fps in 24 30 60 29.97 23.976; do
  output=$(av1ify --dry-run --fps "$fps" "$TEST_DIR/input.avi" 2>&1 || true)
  if [[ "$output" != *"無効なfps"* ]]; then
    printf '✓ FPS %s is valid\n' "$fps"
  else
    printf '✗ FPS %s should be valid\n' "$fps"
  fi
done

# Test 23: 環境変数 AV1_RESOLUTION
printf '\n## Test 23: AV1_RESOLUTION environment variable\n'
TEST_DIR="$TEST_TMP/test23"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(AV1_RESOLUTION=720p av1ify --dry-run "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=720p" "AV1_RESOLUTION env var works"

# Test 24: 環境変数 AV1_FPS
printf '\n## Test 24: AV1_FPS environment variable\n'
TEST_DIR="$TEST_TMP/test24"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(AV1_FPS=24 av1ify --dry-run "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "fps=24" "AV1_FPS env var works"

# Test 25: CLIオプションが環境変数より優先される
printf '\n## Test 25: CLI option takes priority over env var\n'
TEST_DIR="$TEST_TMP/test25"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(AV1_RESOLUTION=480p AV1_FPS=30 av1ify --dry-run -r 1080p --fps 60 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=1080p" "CLI -r overrides AV1_RESOLUTION"
assert_contains "$output" "fps=60" "CLI --fps overrides AV1_FPS"

# Test 26: --resolution オプションで実際にファイル処理
printf '\n## Test 26: Resolution option creates output file with tag\n'
TEST_DIR="$TEST_TMP/test26"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify -r 720p "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-720p-enc.mp4" "Output file has resolution tag"

# Test 27: --fps オプションで実際にファイル処理
printf '\n## Test 27: FPS option creates output file with tag\n'
TEST_DIR="$TEST_TMP/test27"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
av1ify --fps 24 "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-24fps-enc.mp4" "Output file has fps tag"

# Test 28: --resolution と --fps の組み合わせで実際にファイル処理
printf '\n## Test 28: Resolution and FPS combined creates output file\n'
TEST_DIR="$TEST_TMP/test28"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify -r 720p --fps 24 "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-720p-24fps-enc.mp4" "Output file has both resolution and fps tags"

# Test 29: アップスケール防止 — 同解像度はスキップ
printf '\n## Test 29: Upscale prevention - same resolution is skipped\n'
TEST_DIR="$TEST_TMP/test29"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# mock は 1920x1080（短辺=1080）。-r 1080p 指定 → 短辺が同じなのでスキップ
output=$(av1ify -r 1080p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "解像度変更をスキップ" "Skips resolution when source short side equals target"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output file has no resolution tag (no upscale)"
assert_file_not_exists "$TEST_DIR/input-1080p-enc.mp4" "No 1080p-tagged file created"

# Test 30: アップスケール防止 — 元が低解像度の場合もスキップ
printf '\n## Test 30: Upscale prevention - lower resolution source is skipped\n'
TEST_DIR="$TEST_TMP/test30"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# mock は 1920x1080（短辺=1080）。-r 1440p 指定 → 短辺1080 < 1440 なのでスキップ
output=$(av1ify -r 1440p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "解像度変更をスキップ" "Skips resolution when source is lower than target"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output file has no resolution tag (no upscale)"

# Test 31: ダウンスケールは正常動作
printf '\n## Test 31: Downscale still works\n'
TEST_DIR="$TEST_TMP/test31"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# mock は 1920x1080（短辺=1080）。-r 720p 指定 → 短辺1080 > 720 なのでダウンスケール
MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify -r 720p "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-720p-enc.mp4" "Downscale creates file with resolution tag"

# Test 32: 縦長動画 — 短辺（width）で判定される
printf '\n## Test 32: Portrait video - short side (width) is used for resolution check\n'
TEST_DIR="$TEST_TMP/test32"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# mock を縦長に設定: 1080x1920（短辺=width=1080）。-r 1080p → スキップ
output=$(MOCK_WIDTH=1080 MOCK_HEIGHT=1920 av1ify -r 1080p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "解像度変更をスキップ" "Portrait 1080p is correctly detected as 1080p"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Portrait video: no upscale, no resolution tag"

# Test 33: 縦長動画 — ダウンスケール時は scale=W:-2 が使われる
printf '\n## Test 33: Portrait video - downscale uses scale=W:-2\n'
TEST_DIR="$TEST_TMP/test33"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# mock を縦長4K: 2160x3840（短辺=width=2160）。-r 1080p → ダウンスケール
output=$(MOCK_WIDTH=2160 MOCK_HEIGHT=3840 av1ify --dry-run -r 1080p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
# dry-run ではファイルを参照しないため解像度チェックはされない。実行テストへ
TEST_DIR="$TEST_TMP/test33b"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
MOCK_WIDTH=2160 MOCK_HEIGHT=3840 MOCK_OUTPUT_WIDTH=1080 MOCK_OUTPUT_HEIGHT=1920 av1ify -r 1080p "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-1080p-enc.mp4" "Portrait 4K downscaled to 1080p creates tagged file"

# Test 34: --denoise オプションのヘルプメッセージ
printf '\n## Test 34: Help message includes --denoise option\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "--denoise" "Help message contains --denoise option"
assert_contains "$help_output" "ノイズ除去" "Help message describes noise reduction"

# Test 35: --denoise オプション (dry-run)
printf '\n## Test 35: Denoise option with dry-run\n'
TEST_DIR="$TEST_TMP/test35"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --denoise medium "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "denoise=medium" "Dry-run shows denoise=medium"

# Test 36: 無効な denoise 指定
printf '\n## Test 36: Invalid denoise validation\n'
TEST_DIR="$TEST_TMP/test36"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --denoise invalid "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効なdenoise指定" "Reports invalid denoise value"

# Test 37: --denoise オプションで実際にファイル処理
printf '\n## Test 37: Denoise option creates output file with tag\n'
TEST_DIR="$TEST_TMP/test37"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
av1ify --denoise light "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-dn1-enc.mp4" "Output file has denoise tag (dn1)"

# Test 38: --resolution と --denoise の組み合わせ
printf '\n## Test 38: Resolution and denoise combined\n'
TEST_DIR="$TEST_TMP/test38"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify -r 720p --denoise medium "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-720p-dn2-enc.mp4" "Output file has both resolution and denoise tags"

# Test 39: 解像度オプション指定時にソース解像度が取得できない場合はエラー
printf '\n## Test 39: Resolution option errors when source resolution unavailable\n'
TEST_DIR="$TEST_TMP/test39"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_WIDTH="" MOCK_HEIGHT="" av1ify -r 720p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "解像度を取得できません" "Reports error when source resolution unavailable"
assert_file_not_exists "$TEST_DIR/input-720p-enc.mp4" "No output file created when resolution unavailable"

# Test 40: --compact オプションのヘルプメッセージ
printf '\n## Test 40: Help message includes --compact option\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "--compact" "Help message contains --compact option"
assert_contains "$help_output" "720p" "Help message mentions 720p for compact"
assert_contains "$help_output" "30fps" "Help message mentions 30fps for compact"

# Test 41: --compact オプション (dry-run)
printf '\n## Test 41: Compact option with dry-run\n'
TEST_DIR="$TEST_TMP/test41"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --compact "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=720p" "Compact dry-run shows resolution=720p"
assert_contains "$output" "fps=30" "Compact dry-run shows fps=30"

# Test 42: --compact で実際にファイル処理
printf '\n## Test 42: Compact option creates output file with tags\n'
TEST_DIR="$TEST_TMP/test42"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# mock を 60fps にして両方のタグが付くことを確認
MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-720p-30fps-aac96k-enc.mp4" "Compact creates file with 720p, 30fps and aac96k tags"

# Test 43: --compact + 明示的な -r で解像度だけ上書き
printf '\n## Test 43: Compact with explicit resolution override\n'
TEST_DIR="$TEST_TMP/test43"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --compact -r 480p "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=480p" "Compact + explicit -r uses 480p"
assert_contains "$output" "fps=30" "Compact + explicit -r still uses 30fps"

# Test 44: --compact + 明示的な --fps で fps だけ上書き
printf '\n## Test 44: Compact with explicit fps override\n'
TEST_DIR="$TEST_TMP/test44"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --compact --fps 24 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=720p" "Compact + explicit --fps still uses 720p"
assert_contains "$output" "fps=24" "Compact + explicit --fps uses 24"

# Test 45: --compact はアップスケール防止が効く
printf '\n## Test 45: Compact respects upscale prevention\n'
TEST_DIR="$TEST_TMP/test45"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# mock は 480x854（短辺=480）、60fps。--compact → 720p 指定だが 480 < 720 なのでスキップ、fps は適用
output=$(MOCK_WIDTH=480 MOCK_HEIGHT=854 MOCK_FPS="60/1" av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "解像度変更をスキップ" "Compact skips upscale for low-res source"
assert_file_exists "$TEST_DIR/input-30fps-aac96k-enc.mp4" "Compact low-res: fps and aac96k tags, no resolution tag"

# Test 46: -c 省略形が --compact と同じ動作
printf '\n## Test 46: Short -c alias for --compact\n'
TEST_DIR="$TEST_TMP/test46"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -c "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=720p" "-c dry-run shows resolution=720p"
assert_contains "$output" "fps=30" "-c dry-run shows fps=30"

# Test 47: 不明なオプションでエラー終了
printf '\n## Test 47: Unknown option causes error\n'
TEST_DIR="$TEST_TMP/test47"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify --unknown-option "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "不明なオプション" "Reports unknown option error"
(( exit_code != 0 )) && printf '✓ Exit code is non-zero (%d)\n' "$exit_code" || printf '✗ Exit code should be non-zero (got %d)\n' "$exit_code"

# Test 48: -x のような短い不明オプションでもエラー
printf '\n## Test 48: Unknown short option causes error\n'
unsetopt err_exit
output=$(av1ify -x "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "不明なオプション" "Reports unknown short option error"
(( exit_code != 0 )) && printf '✓ Exit code is non-zero (%d)\n' "$exit_code" || printf '✗ Exit code should be non-zero (got %d)\n' "$exit_code"

# Test 49: -f は引き続き正常に動作する
printf '\n## Test 49: -f option still works after unknown option guard\n'
TEST_DIR="$TEST_TMP/test49"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.avi"
cat > "$TEST_DIR/list.txt" <<LISTEOF
$TEST_DIR/video.avi
LISTEOF
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify -f "$TEST_DIR/list.txt" 2>&1 || true)
setopt err_exit
assert_file_exists "$TEST_DIR/video-enc.mp4" "-f option processes files correctly"

# Test 50: compact モードで音声が96kbps超ならAAC再エンコード
printf '\n## Test 50: Compact re-encodes audio when bitrate > 96kbps\n'
TEST_DIR="$TEST_TMP/test50"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# デフォルトの MOCK_AUDIO_BITRATE=248000 (248kbps > 96kbps)
output=$(MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "aac 96k へ再エンコード" "Compact re-encodes audio to 96k"
assert_file_exists "$TEST_DIR/input-720p-30fps-aac96k-enc.mp4" "Compact output has aac96k tag"

# Test 51: compact モードで音声が96kbps以下ならcopy
printf '\n## Test 51: Compact copies audio when bitrate <= 96kbps\n'
TEST_DIR="$TEST_TMP/test51"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_AUDIO_BITRATE=96000 MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "copy" "Compact copies audio when <= 96kbps"
assert_file_exists "$TEST_DIR/input-720p-30fps-enc.mp4" "Compact output has no aac tag when copying"

# Test 52: 非compact モードでは音声は常にcopy（許可コーデックの場合）
printf '\n## Test 52: Non-compact always copies audio for allowed codecs\n'
TEST_DIR="$TEST_TMP/test52"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "音声: copy" "Non-compact copies audio regardless of bitrate"

# Test 53: compact dry-runで音声再エンコードが表示される
printf '\n## Test 53: Compact dry-run shows audio re-encode plan\n'
TEST_DIR="$TEST_TMP/test53"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --compact "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "compact" "Compact dry-run mentions compact audio"

# Test 54: fps キャップ — ソースが30fps以下ならfps変更スキップ
printf '\n## Test 54: FPS cap - skip when source <= target\n'
TEST_DIR="$TEST_TMP/test54"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# mock は 30000/1001 (29.97fps)。--fps 30 → 29.97 <= 30 なのでスキップ
output=$(MOCK_FPS="30000/1001" av1ify --fps 30 "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "fps変更をスキップ" "Skips fps change when source (29.97) <= target (30)"
assert_file_exists "$TEST_DIR/input-enc.mp4" "No fps tag when skipped"
assert_file_not_exists "$TEST_DIR/input-30fps-enc.mp4" "No 30fps-tagged file created"

# Test 55: fps キャップ — ソースが60fpsなら30fpsに落とす
printf '\n## Test 55: FPS cap - apply when source > target\n'
TEST_DIR="$TEST_TMP/test55"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_FPS="60000/1001" av1ify --fps 30 "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "→ 30fps" "Applies fps change when source (59.94) > target (30)"
assert_file_exists "$TEST_DIR/input-30fps-enc.mp4" "Output file has fps tag"

# Test 56: --compact + 29.97fps ソース → fpsスキップ、解像度のみ適用
printf '\n## Test 56: Compact with 29.97fps source skips fps\n'
TEST_DIR="$TEST_TMP/test56"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_FPS="30000/1001" MOCK_AUDIO_BITRATE=96000 MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "fps変更をスキップ" "Compact skips fps for 29.97fps source"
assert_file_exists "$TEST_DIR/input-720p-enc.mp4" "Only resolution tag, no fps tag"

# Test 57: --compact + 60fps ソース → 両方適用
printf '\n## Test 57: Compact with 60fps source applies both\n'
TEST_DIR="$TEST_TMP/test57"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_FPS="60000/1001" MOCK_AUDIO_BITRATE=96000 MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_file_exists "$TEST_DIR/input-720p-30fps-enc.mp4" "Compact with 60fps: both tags applied"

# Test 58: 部分一致 — "7" は "720p" に解決
printf '\n## Test 58: Partial match - "7" resolves to 720p\n'
TEST_DIR="$TEST_TMP/test58"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 7 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "720p に解決しました" "Partial match '7' resolves to 720p"
assert_contains "$output" "resolution=720p" "Dry-run shows resolved resolution=720p"

# Test 59: 部分一致 — "10" は "1080p" に解決
printf '\n## Test 59: Partial match - "10" resolves to 1080p\n'
TEST_DIR="$TEST_TMP/test59"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 10 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "1080p に解決しました" "Partial match '10' resolves to 1080p"
assert_contains "$output" "resolution=1080p" "Dry-run shows resolved resolution=1080p"

# Test 60: 部分一致 — "14" は "1440p" に解決
printf '\n## Test 60: Partial match - "14" resolves to 1440p\n'
TEST_DIR="$TEST_TMP/test60"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 14 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "1440p に解決しました" "Partial match '14' resolves to 1440p"
assert_contains "$output" "resolution=1440p" "Dry-run shows resolved resolution=1440p"

# Test 61: 部分一致（複数候補） — "4" は "480p" に解決（配列の先頭候補を選択）
printf '\n## Test 61: Partial match with multiple candidates - "4" resolves to 480p\n'
TEST_DIR="$TEST_TMP/test61"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 4 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "480p に解決しました" "Partial match '4' resolves to 480p (first candidate)"
assert_contains "$output" "resolution=480p" "Dry-run shows resolved resolution=480p"

# Test 62: 環境変数 AV1_RESOLUTION での部分一致
printf '\n## Test 62: Env var AV1_RESOLUTION partial match - "7" resolves to 720p\n'
TEST_DIR="$TEST_TMP/test62"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(AV1_RESOLUTION=7 av1ify --dry-run "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "720p に解決しました" "Env var partial match '7' resolves to 720p"
assert_contains "$output" "resolution=720p" "Dry-run shows resolved resolution=720p via env var"

# Test 63: 大文字入力 "4K" は "4k" に解決
printf '\n## Test 63: Uppercase input "4K" resolves to 4k\n'
TEST_DIR="$TEST_TMP/test63"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 4K "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=4k" "Uppercase '4K' resolves to 4k"

# Test 64: 大文字入力 "720P" は "720p" に解決
printf '\n## Test 64: Uppercase input "720P" resolves to 720p\n'
TEST_DIR="$TEST_TMP/test64"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 720P "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=720p" "Uppercase '720P' resolves to 720p"

# Test 65: 非copyコーデックで低ビットレート → アップスケール防止でキャップ
printf '\n## Test 65: Non-copy codec low bitrate - caps to source bitrate\n'
TEST_DIR="$TEST_TMP/test65"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# vorbis 48kbps → AAC 96k ではなく 48k にキャップされるべき
output=$(MOCK_ACODEC=vorbis MOCK_AUDIO_BITRATE=48000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "アップスケール防止" "Non-copy low bitrate triggers upscale prevention"
assert_contains "$output" "aac 48k" "Bitrate is capped to 48k"

# Test 66: 非copyコーデックで高ビットレート → 通常の96kで再エンコード
printf '\n## Test 66: Non-copy codec high bitrate - uses default target bitrate\n'
TEST_DIR="$TEST_TMP/test66"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# vorbis 192kbps → 96k に再エンコード（通常動作）
output=$(MOCK_ACODEC=vorbis MOCK_AUDIO_BITRATE=192000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "aac 96k" "Non-copy high bitrate uses default 96k"
if [[ "$output" != *"アップスケール防止"* ]]; then
  printf '✓ No upscale prevention message for high bitrate\n'
else
  printf '✗ Should not show upscale prevention for high bitrate\n'
fi

# Test 67: 非copyコーデックで極低ビットレート → 最低32kフロア
printf '\n## Test 67: Non-copy codec very low bitrate - minimum 32k floor\n'
TEST_DIR="$TEST_TMP/test67"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# vorbis 16kbps → 32k にフロア
output=$(MOCK_ACODEC=vorbis MOCK_AUDIO_BITRATE=16000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "aac 32k" "Very low bitrate is floored to 32k"

# Test 68: 非copyコーデックでビットレート不明 → デフォルトの96kを使用
printf '\n## Test 68: Non-copy codec unknown bitrate - uses default\n'
TEST_DIR="$TEST_TMP/test68"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_AUDIO_BITRATE="" av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ビットレート不明" "Shows unknown bitrate message"

printf '\n=== Options Tests Completed ===\n'
