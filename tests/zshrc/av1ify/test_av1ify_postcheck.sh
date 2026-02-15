#!/usr/bin/env zsh
# shellcheck shell=bash
# av1ify ポストチェックテスト (Test 65-79)
# 再生時間ズレ、フレーム数、解像度、ファイルサイズ、コーデックの変換後チェック

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Postcheck Tests (65-79) ===\n\n'

# Test 65: 再生時間ズレ検出 — 出力がソースより大きくずれている場合に警告
printf '## Test 65: Duration mismatch detection\n'
TEST_DIR="$TEST_TMP/test65"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# ソース=10.0s, 出力=5.0s → Δ=5.0s > 2.0s(デフォルト閾値）で警告
output=$(MOCK_FORMAT_DURATION=10.0 MOCK_OUTPUT_FORMAT_DURATION=5.0 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "再生時間ズレ" "Detects duration mismatch between source and output"
assert_contains "$output" "check_ng" "Output is marked as check_ng"

# Test 66: 再生時間が許容範囲内なら警告なし
printf '\n## Test 66: Duration within tolerance - no warning\n'
TEST_DIR="$TEST_TMP/test66"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# ソース=10.0s, 出力=9.5s → Δ=0.5s < 2.0s で正常
output=$(MOCK_FORMAT_DURATION=10.0 MOCK_OUTPUT_FORMAT_DURATION=9.5 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"再生時間ズレ"* ]]; then
  printf '✓ No duration warning within tolerance\n'
else
  printf '✗ Should not warn when duration difference is within tolerance\n'
fi

# Test 67: AV1IFY_DURATION_TOLERANCE で閾値をカスタマイズ
printf '\n## Test 67: Custom duration tolerance via AV1IFY_DURATION_TOLERANCE\n'
TEST_DIR="$TEST_TMP/test67"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# Δ=1.5s, デフォルト閾値(2.0s)では通るが閾値を1.0sに下げると検出
output=$(AV1IFY_DURATION_TOLERANCE=1.0 MOCK_FORMAT_DURATION=10.0 MOCK_OUTPUT_FORMAT_DURATION=8.5 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "再生時間ズレ" "Custom tolerance detects smaller duration mismatch"

# Test 68: フレーム数不一致の検出
printf '\n## Test 68: Frame count mismatch detection\n'
TEST_DIR="$TEST_TMP/test68"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# ソース=300フレーム, 出力=250フレーム → 不一致で警告
output=$(MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=250 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "フレーム数不一致" "Detects frame count mismatch"
assert_contains "$output" "check_ng" "Output is marked as check_ng"

# Test 69: フレーム数一致なら警告なし
printf '\n## Test 69: Frame count match - no warning\n'
TEST_DIR="$TEST_TMP/test69"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=300 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム数不一致"* ]]; then
  printf '✓ No frame count warning when counts match\n'
else
  printf '✗ Should not warn when frame counts match\n'
fi

# Test 70: fps変更時はフレーム数チェックをスキップ
printf '\n## Test 70: Frame count check skipped when fps changed\n'
TEST_DIR="$TEST_TMP/test70"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# fps変更あり(60→30)の場合、フレーム数が異なっても警告しない
output=$(MOCK_FPS="60000/1001" MOCK_NB_FRAMES=600 MOCK_OUTPUT_NB_FRAMES=300 av1ify --fps 30 "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム数不一致"* ]]; then
  printf '✓ No frame count warning when fps changed\n'
else
  printf '✗ Should not check frame count when fps is changed\n'
fi

# Test 71: 出力解像度不一致の検出
printf '\n## Test 71: Output resolution mismatch detection\n'
TEST_DIR="$TEST_TMP/test71"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# -r 720p指定だが出力が1080pのまま → 不一致で警告
output=$(MOCK_OUTPUT_WIDTH=1920 MOCK_OUTPUT_HEIGHT=1080 av1ify -r 720p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "解像度不一致" "Detects output resolution mismatch"

# Test 72: 出力解像度が期待通りなら警告なし
printf '\n## Test 72: Output resolution match - no warning\n'
TEST_DIR="$TEST_TMP/test72"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# -r 720p指定で出力も720p → 正常
output=$(MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify -r 720p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"解像度不一致"* ]]; then
  printf '✓ No resolution warning when output matches expected\n'
else
  printf '✗ Should not warn when resolution matches\n'
fi

# Test 73: 解像度指定なしでは解像度チェックをスキップ
printf '\n## Test 73: Resolution check skipped when no -r specified\n'
TEST_DIR="$TEST_TMP/test73"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# -r なし。出力解像度が何であっても警告しない
output=$(MOCK_OUTPUT_WIDTH=640 MOCK_OUTPUT_HEIGHT=480 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"解像度不一致"* ]]; then
  printf '✓ No resolution check when -r not specified\n'
else
  printf '✗ Should not check resolution when -r is not specified\n'
fi

# Test 74: 縦長出力の解像度チェック（短辺=widthで判定）
printf '\n## Test 74: Portrait output resolution check uses short side\n'
TEST_DIR="$TEST_TMP/test74"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# ソース: 2160x3840(縦長4K), -r 1080p指定, 出力が1080x1920 → 短辺=1080=期待値で正常
output=$(MOCK_WIDTH=2160 MOCK_HEIGHT=3840 MOCK_OUTPUT_WIDTH=1080 MOCK_OUTPUT_HEIGHT=1920 av1ify -r 1080p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"解像度不一致"* ]]; then
  printf '✓ Portrait resolution check uses short side correctly\n'
else
  printf '✗ Portrait resolution should use short side (width)\n'
fi

# Test 75: ファイルサイズ異常の検出
printf '\n## Test 75: File size anomaly detection\n'
TEST_DIR="$TEST_TMP/test75"
mkdir -p "$TEST_DIR"
# ソースを十分大きく (100KB)
dd if=/dev/zero of="$TEST_DIR/input.avi" bs=1024 count=100 2>/dev/null
cd "$TEST_DIR"
# ffmpegモックの出力は "mock video data" (15バイト) → ratio≈0.00015 < 0.001
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ファイルサイズ異常" "Detects abnormally small output file"
assert_contains "$output" "check_ng" "Output is marked as check_ng"

# Test 76: ファイルサイズが妥当なら警告なし
printf '\n## Test 76: File size normal - no warning\n'
TEST_DIR="$TEST_TMP/test76"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
# ソースも小さい(12B)、モック出力も小さい(15B) → ratio≈1.25 > 0.001 で正常
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"ファイルサイズ異常"* ]]; then
  printf '✓ No file size warning for normal ratio\n'
else
  printf '✗ Should not warn when file size ratio is normal\n'
fi

# Test 77: AV1IFY_MIN_SIZE_RATIO で閾値をカスタマイズ
printf '\n## Test 77: Custom file size ratio via AV1IFY_MIN_SIZE_RATIO\n'
TEST_DIR="$TEST_TMP/test77"
mkdir -p "$TEST_DIR"
# ソース200バイト、出力15バイト → ratio≈0.075。閾値を0.1にすると検出
dd if=/dev/zero of="$TEST_DIR/input.avi" bs=1 count=200 2>/dev/null
cd "$TEST_DIR"
unsetopt err_exit
output=$(AV1IFY_MIN_SIZE_RATIO=0.1 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ファイルサイズ異常" "Custom ratio threshold detects small output"

# Test 78: 出力映像コーデック不一致の検出
printf '\n## Test 78: Output video codec mismatch detection\n'
TEST_DIR="$TEST_TMP/test78"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# 出力コーデックが h264 → av1 でないので警告
output=$(MOCK_OUTPUT_VCODEC=h264 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "映像コーデック不一致" "Detects non-AV1 output codec"
assert_contains "$output" "check_ng" "Output is marked as check_ng"

# Test 79: 出力コーデックが av1 なら警告なし
printf '\n## Test 79: Output video codec is av1 - no warning\n'
TEST_DIR="$TEST_TMP/test79"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_OUTPUT_VCODEC=av1 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"映像コーデック不一致"* ]]; then
  printf '✓ No codec warning when output is av1\n'
else
  printf '✗ Should not warn when output codec is av1\n'
fi

printf '\n=== Postcheck Tests Completed ===\n'
