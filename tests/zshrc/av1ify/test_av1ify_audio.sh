#!/usr/bin/env zsh
# shellcheck shell=bash
# av1ify 音声処理テスト (Test 50-53, 65-72)
# compact音声判定、非copyコーデックのアップスケール防止、サンプルレート/チャンネル数調整

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Audio Tests (50-53, 65-72) ===\n\n'

# Test 50: compact モードで音声が96kbps超ならAAC再エンコード
printf '## Test 50: Compact re-encodes audio when bitrate > 96kbps\n'
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
assert_not_contains "$output" "アップスケール防止" "No upscale prevention message for high bitrate"

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

# Test 69: monoソース → チャンネル数をアップスケールしない
printf '\n## Test 69: Mono source - no channel upscale\n'
TEST_DIR="$TEST_TMP/test69"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_CHANNELS=1 MOCK_AUDIO_BITRATE=48000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ch:1" "Mono source: adjusts to 1 channel"

# Test 70: 低サンプルレートソース → サンプルレートをアップスケールしない
printf '\n## Test 70: Low sample rate source - no sample rate upscale\n'
TEST_DIR="$TEST_TMP/test70"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_SAMPLE_RATE=22050 MOCK_AUDIO_BITRATE=48000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ar:22050Hz" "Low sample rate: adjusts to source rate"

# Test 71: stereo 48kHz ソース → 調整メッセージなし（上限と同じ）
printf '\n## Test 71: Standard stereo 48kHz - no adjustment message\n'
TEST_DIR="$TEST_TMP/test71"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_CHANNELS=2 MOCK_SAMPLE_RATE=48000 MOCK_AUDIO_BITRATE=192000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "ソースに合わせて調整" "No adjustment message for standard stereo 48kHz"

# Test 72: mono 22050Hz ソース → 両方調整
printf '\n## Test 72: Mono 22050Hz source - both adjusted\n'
TEST_DIR="$TEST_TMP/test72"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_CHANNELS=1 MOCK_SAMPLE_RATE=22050 MOCK_AUDIO_BITRATE=32000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ar:22050Hz" "Both adjusted: sample rate"
assert_contains "$output" "ch:1" "Both adjusted: channels"

# Test 73: compact + mono低サンプルレート → aac_ar/aac_acが反映される
printf '\n## Test 73: Compact with mono low sample rate source\n'
TEST_DIR="$TEST_TMP/test73"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_CHANNELS=1 MOCK_SAMPLE_RATE=22050 MOCK_AUDIO_BITRATE=248000 MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ch:1" "Compact mono: channel adjusted"
assert_contains "$output" "ar:22050Hz" "Compact low sample rate: adjusted"
assert_contains "$output" "aac 96k へ再エンコード" "Compact still re-encodes to 96k"

printf '\n=== Audio Tests Completed ===\n'
