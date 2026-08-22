#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify 音声処理テスト (Test 50-53, 65-72)
# compact音声判定、非copyコーデックのアップスケール防止、サンプルレート/チャンネル数調整

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Audio Tests (50-53, 65-72) ===\n\n'

# Test 50: 閾値 (96k x 1.15 = 110400bps) 超なら AAC 再エンコード (compact でも通常と同じ判定)
printf '## Test 50: Compact re-encodes audio above the reencode threshold\n'
TEST_DIR="$TEST_TMP/test50"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# 248000bps > 閾値 110400bps
output=$(MOCK_AUDIO_BITRATE=248000 MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "aac 96k へ再エンコード" "Compact re-encodes audio to 96k"
assert_file_exists "$TEST_DIR/input-720p-30fps-aac96k-enc.mp4" "Compact output has aac96k tag"

# Test 51: 閾値以下なら copy (再エンコードしても削減が小さいため)
printf '\n## Test 51: Compact copies audio at or below the reencode threshold\n'
TEST_DIR="$TEST_TMP/test51"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_AUDIO_BITRATE=96000 MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "音声: copy" "Compact copies audio at/below threshold"
assert_not_contains "$output" "へ再エンコード" "Compact does not re-encode at/below threshold"
assert_file_exists "$TEST_DIR/input-720p-30fps-enc.mp4" "Compact output has no aac tag when copying"

# Test 52: 通常モードでも閾値超の copy 可能コーデックは再エンコードする
# (以前は「copy 可能なら無条件 copy」だったため、圧縮したい高ビットレート AAC が素通りしていた)
printf '\n## Test 52: Non-compact re-encodes allowed codec above threshold\n'
TEST_DIR="$TEST_TMP/test52"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# aac 248000bps > 閾値 110400bps
output=$(MOCK_AUDIO_BITRATE=248000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "aac 96k へ再エンコード" "Non-compact re-encodes aac above threshold"
assert_file_exists "$TEST_DIR/input-aac96k-enc.mp4" "Non-compact output has aac96k tag"

# Test 52b: 通常モードで閾値以下の copy 可能コーデックは copy のまま
printf '\n## Test 52b: Non-compact copies allowed codec at/below threshold\n'
TEST_DIR="$TEST_TMP/test52b"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_AUDIO_BITRATE=100000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "音声: copy" "Non-compact copies aac at/below threshold"
assert_not_contains "$output" "へ再エンコード" "Non-compact does not re-encode at/below threshold"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Non-compact copy output has no aac tag"

# Test 52c: 境界値 — 閾値ちょうど (110400bps) は copy、1bps 超えたら再エンコード
printf '\n## Test 52c: Threshold boundary is exclusive\n'
TEST_DIR="$TEST_TMP/test52c"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_AUDIO_BITRATE=110400 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "音声: copy" "Exactly at threshold copies"

TEST_DIR="$TEST_TMP/test52c2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_AUDIO_BITRATE=110401 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "へ再エンコード" "One bps above threshold re-encodes"

# Test 52d: AV1_AUDIO_REENCODE_MARGIN で閾値を動かせる
printf '\n## Test 52d: AV1_AUDIO_REENCODE_MARGIN shifts the threshold\n'
TEST_DIR="$TEST_TMP/test52d"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# margin=3.0 → 閾値 288000bps。248000 は下回るので copy になる (既定 1.15 なら再エンコード)
output=$(AV1_AUDIO_REENCODE_MARGIN=3.0 MOCK_AUDIO_BITRATE=248000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "音声: copy" "Large margin keeps copy"

TEST_DIR="$TEST_TMP/test52d2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# margin=1.0 → 閾値 96000bps。100000 は上回るので再エンコード (既定 1.15 なら copy)
output=$(AV1_AUDIO_REENCODE_MARGIN=1.0 MOCK_AUDIO_BITRATE=100000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "へ再エンコード" "Small margin forces re-encode"

# Test 52e: ソースビットレート不明の copy 可能コーデックは copy (安全側)
printf '\n## Test 52e: Unknown bitrate on copyable codec falls back to copy\n'
TEST_DIR="$TEST_TMP/test52e"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_AUDIO_BITRATE="" av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "音声: copy" "Unknown bitrate on copyable codec copies"

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
assert_contains "$output" "mono のためステレオへのアップスケールをスキップ" "Mono source: skips stereo upscale"

# Test 70: 低サンプルレートソース → サンプルレートをアップスケールしない
printf '\n## Test 70: Low sample rate source - no sample rate upscale\n'
TEST_DIR="$TEST_TMP/test70"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_SAMPLE_RATE=22050 MOCK_AUDIO_BITRATE=48000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "22050Hz のため 48000Hz へのアップスケールをスキップ" "Low sample rate: skips upscale"

# Test 71: stereo 48kHz ソース → 調整メッセージなし（上限と同じ）
printf '\n## Test 71: Standard stereo 48kHz - no adjustment message\n'
TEST_DIR="$TEST_TMP/test71"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_CHANNELS=2 MOCK_SAMPLE_RATE=48000 MOCK_AUDIO_BITRATE=192000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "アップスケールをスキップ" "No skip message for standard stereo 48kHz"

# Test 72: mono 22050Hz ソース → 両方スキップ
printf '\n## Test 72: Mono 22050Hz source - both skipped\n'
TEST_DIR="$TEST_TMP/test72"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_CHANNELS=1 MOCK_SAMPLE_RATE=22050 MOCK_AUDIO_BITRATE=32000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "22050Hz のため" "Both skipped: sample rate"
assert_contains "$output" "mono のため" "Both skipped: channels"

# Test 73: compact + mono低サンプルレート → アップスケールスキップが反映される
printf '\n## Test 73: Compact with mono low sample rate source\n'
TEST_DIR="$TEST_TMP/test73"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_CHANNELS=1 MOCK_SAMPLE_RATE=22050 MOCK_AUDIO_BITRATE=248000 MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "mono のため" "Compact mono: stereo upscale skipped"
assert_contains "$output" "22050Hz のため" "Compact low sample rate: upscale skipped"
assert_contains "$output" "aac 96k へ再エンコード" "Compact still re-encodes to 96k"

# Test 74: 非copyコーデックで音声パラメータ取得失敗 → copyフォールバック + auderrタグ
printf '\n## Test 74: Non-copy codec param error - copy fallback with auderr tag\n'
TEST_DIR="$TEST_TMP/test74"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_SAMPLE_RATE="" MOCK_AUDIO_BITRATE=48000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "パラメータ取得失敗" "Non-copy param error: shows fallback message"
assert_contains "$output" "copy にフォールバック" "Non-copy param error: falls back to copy"
assert_file_exists "$TEST_DIR/input-auderr-enc.mp4" "Non-copy param error: output has auderr tag"

# Test 75: compactモードで音声パラメータ取得失敗 → copyフォールバック + auderrタグ
printf '\n## Test 75: Compact param error - copy fallback with auderr tag\n'
TEST_DIR="$TEST_TMP/test75"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_SAMPLE_RATE="" MOCK_AUDIO_BITRATE=248000 MOCK_FPS="60/1" MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "パラメータ取得失敗" "Compact param error: shows fallback message"
assert_contains "$output" "copy にフォールバック" "Compact param error: falls back to copy"
assert_file_exists "$TEST_DIR/input-720p-30fps-auderr-enc.mp4" "Compact param error: output has auderr tag"

# Test 76: チャンネル数のみ取得失敗 → copyフォールバック + auderrタグ
printf '\n## Test 76: Channels-only param error - copy fallback with auderr tag\n'
TEST_DIR="$TEST_TMP/test76"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC=vorbis MOCK_CHANNELS="" MOCK_SAMPLE_RATE=22050 MOCK_AUDIO_BITRATE=48000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "パラメータ取得失敗" "Channels-only error: shows fallback message"
assert_file_exists "$TEST_DIR/input-auderr-enc.mp4" "Channels-only error: output has auderr tag"

# Test 52f: 不正な AV1_AUDIO_REENCODE_MARGIN は fail-fast する
# 背景: awk は非数値を数値文脈で 0 と解釈するため ("abc" * 96000 = 0)、検証しないと
# typo が閾値 0 = 全ソース再エンコードに化ける。しかも閾値 0 は数値として妥当なので
# 算出後の検査では検出できない。入力側で弾く必要がある。
printf '\n## Test 52f: Invalid AV1_AUDIO_REENCODE_MARGIN fails fast\n'
TEST_DIR="$TEST_TMP/test52f"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
for bad in "abc" "1,15" "0" "-1"; do
  unsetopt err_exit
  output=$(AV1_AUDIO_REENCODE_MARGIN="$bad" av1ify "$TEST_DIR/input.avi" 2>&1)
  rc=$?
  setopt err_exit
  if [[ "$rc" != "0" ]] && [[ "$output" == *"無効なAV1_AUDIO_REENCODE_MARGIN指定"* ]]; then
    printf '✓ Rejects invalid margin %s\n' "$bad"
  else
    printf '✗ Did not reject invalid margin %s (rc=%s)\n' "$bad" "$rc"
  fi
  assert_file_not_exists "$TEST_DIR/input-enc.mp4" "No output for invalid margin $bad"
  assert_file_not_exists "$TEST_DIR/input-aac96k-enc.mp4" "No aac output for invalid margin $bad"
done

# 正常値は通ること (上の拒否が過剰適用でないことの確認)
unsetopt err_exit
output=$(AV1_AUDIO_REENCODE_MARGIN=1.15 MOCK_AUDIO_BITRATE=96000 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "音声: copy" "Valid margin still works"

# Test 77: AAC を選んでいた場合、失敗しても AAC で再試行しない (空振り防止)
# 背景: 失敗時 retry は「失敗 = 音声 copy が原因」という前提の救済策。既に AAC を
# 選んでいたなら 2 回目は同じ引数の空振りにしかならない。ゲートは「コーデックが copy
# 可能か」ではなく「実際に copy を選んだか」で判定する必要がある。
printf '\n## Test 77: No pointless AAC retry when AAC was already chosen\n'
TEST_DIR="$TEST_TMP/test77"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
ARGS_LOG="$TEST_DIR/ffmpeg_args"
unsetopt err_exit
# 248000bps > 閾値 → AAC を選ぶ。その状態で ffmpeg が失敗する
output=$(MOCK_AUDIO_BITRATE=248000 MOCK_FFMPEG_FAIL=1 TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" \
  av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "音声copy失敗" "Does not blame audio copy when AAC was used"
attempts=$(grep -c . "$ARGS_LOG" 2>/dev/null || echo 0)
if [[ "$attempts" == "1" ]]; then
  printf '✓ No second doomed ffmpeg attempt when AAC was already chosen (attempts=%s)\n' "$attempts"
else
  printf '✗ Expected exactly 1 ffmpeg attempt when AAC was chosen, got %s\n' "$attempts"
fi

# Test 78: copy を選んでいた場合は従来どおり AAC で再試行する (Test 77 の過剰適用防止)
printf '\n## Test 78: Copy path still retries with AAC on failure\n'
TEST_DIR="$TEST_TMP/test78"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
ARGS_LOG="$TEST_DIR/ffmpeg_args"
unsetopt err_exit
# 96000bps <= 閾値 → copy を選ぶ。失敗したら AAC で 2 回目を試すのが正しい
output=$(MOCK_AUDIO_BITRATE=96000 MOCK_FFMPEG_FAIL=1 TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" \
  av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "音声copy失敗" "Copy path still reports audio-copy failure"
attempts=$(grep -c . "$ARGS_LOG" 2>/dev/null || echo 0)
if [[ "$attempts" == "2" ]]; then
  printf '✓ Retries exactly once with AAC after a copy failure (attempts=%s)\n' "$attempts"
else
  printf '✗ Expected exactly 2 ffmpeg attempts on the copy path, got %s\n' "$attempts"
fi

printf '\n=== Audio Tests Completed ===\n'
