#!/usr/bin/env zsh
unset CDPATH
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

# Test 69b: フレーム数差が閾値内（Δ≤24）なら警告なし
printf '\n## Test 69b: Frame count difference within tolerance - no warning\n'
TEST_DIR="$TEST_TMP/test69b"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# ソース=300フレーム, 出力=285フレーム → Δ=15 ≤ 24(デフォルト閾値)で正常
output=$(MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=285 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム数不一致"* ]]; then
  printf '✓ No frame count warning when difference is within tolerance\n'
else
  printf '✗ Should not warn when frame count difference ≤ 24\n'
fi

# Test 69c: AV1IFY_FRAME_TOLERANCE で閾値をカスタマイズ
printf '\n## Test 69c: Custom frame tolerance via AV1IFY_FRAME_TOLERANCE\n'
TEST_DIR="$TEST_TMP/test69c"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# Δ=15, デフォルト閾値(24)では通るが閾値を10に下げると検出
output=$(AV1IFY_FRAME_TOLERANCE=10 MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=285 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "フレーム数不一致" "Custom frame tolerance detects smaller difference"

# Test 69d: 長尺では相対許容 (0.5%) が効き、Δ>24 でも警告なし
printf '\n## Test 69d: Relative tolerance absorbs small drift on long videos\n'
TEST_DIR="$TEST_TMP/test69d"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# ソース=45000フレーム (25分@30fps 相当), Δ=88。絶対フロア 24 は超えるが
# 相対許容 45000*0.5%=225 の範囲内 → 警告なし (2026-07-12 の緩和の回帰テスト)
output=$(MOCK_NB_FRAMES=45000 MOCK_OUTPUT_NB_FRAMES=44912 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム数不一致"* ]]; then
  printf '✓ No frame count warning when diff is within relative tolerance\n'
else
  printf '✗ Should not warn when diff ≤ 0.5%% of source frames\n'
fi

# Test 69e: AV1IFY_FRAME_TOLERANCE_PCT で相対許容をカスタマイズ
printf '\n## Test 69e: Custom relative tolerance via AV1IFY_FRAME_TOLERANCE_PCT\n'
TEST_DIR="$TEST_TMP/test69e"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# Δ=88, 既定 (0.5%=225) では通るが 0.1% (=45) に絞ると検出
output=$(AV1IFY_FRAME_TOLERANCE_PCT=0.1 MOCK_NB_FRAMES=45000 MOCK_OUTPUT_NB_FRAMES=44912 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "フレーム数不一致" "Custom relative tolerance detects smaller drift"

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
# ソース100B > モック出力16B → tinyfileにもbiggerにもならない
dd if=/dev/zero of="$TEST_DIR/input.avi" bs=1 count=100 2>/dev/null
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"ファイルサイズ異常"* && "$output" != *"サイズ増加"* ]]; then
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

# Test 77b: サイズ増加の検出 — 出力がソースより大きい場合に警告
printf '\n## Test 77b: File size increase detection\n'
TEST_DIR="$TEST_TMP/test77b"
mkdir -p "$TEST_DIR"
# ソース2B、モック出力16B → out > src でサイズ増加検出
echo -n "x" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "サイズ増加" "Detects output larger than source"
assert_contains "$output" "bigger" "Output filename contains bigger tag"
assert_contains "$output" "check_ng" "Output is marked as check_ng"

# Test 77c: 出力がソースより小さい場合はサイズ増加警告なし
printf '\n## Test 77c: File size decrease - no bigger warning\n'
TEST_DIR="$TEST_TMP/test77c"
mkdir -p "$TEST_DIR"
# ソース100B > モック出力16B → biggerにならない
dd if=/dev/zero of="$TEST_DIR/input.avi" bs=1 count=100 2>/dev/null
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"サイズ増加"* ]]; then
  printf '✓ No size increase warning when output is smaller\n'
else
  printf '✗ Should not warn when output is smaller than source\n'
fi

# Test 77d: サイズ増加時に増加率(%)が表示される
printf '\n## Test 77d: Size increase percentage is shown\n'
TEST_DIR="$TEST_TMP/test77d"
mkdir -p "$TEST_DIR"
# ソース10B、モック出力16B → +60%
dd if=/dev/zero of="$TEST_DIR/input.avi" bs=1 count=10 2>/dev/null
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_FFMPEG_OUTPUT_SIZE=16 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "+60%" "Shows correct percentage increase"

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

# Test 80: __av1ify_is_nonneg_num — 通常の非負小数を受理
# 回帰防止: BSD ERE で `\+` を使うと "repetition-operator operand invalid" になる問題
# (commit 754ce2d の修正: `(\+)?` → `[+]?`) を直接ユニットテスト
printf '\n## Test 80: __av1ify_is_nonneg_num accepts non-negative decimals\n'
unsetopt err_exit
for v in 0 0.5 1 1.5 10 .25 100.0; do
  err=$(__av1ify_is_nonneg_num "$v" 2>&1)
  rc=$?
  if (( rc == 0 )) && [[ -z "$err" ]]; then
    printf '✓ "%s" is accepted (no stderr)\n' "$v"
  else
    printf '✗ "%s" should be accepted (rc=%d, err=%q)\n' "$v" "$rc" "$err"
  fi
done
setopt err_exit

# Test 81: __av1ify_is_nonneg_num — 先頭の `+` を許容（regex 修正の本丸）
# 修正前は `(\+)?` が BSD ERE で invalid となり、`+` を含まない値ですら
# stderr に "repetition-operator operand invalid" を毎回吐いていた
printf '\n## Test 81: __av1ify_is_nonneg_num accepts optional leading +\n'
unsetopt err_exit
for v in +0 +0.5 +1 +1.5 +.25; do
  err=$(__av1ify_is_nonneg_num "$v" 2>&1)
  rc=$?
  if (( rc == 0 )) && [[ -z "$err" ]]; then
    printf '✓ "%s" is accepted (no stderr)\n' "$v"
  else
    printf '✗ "%s" should be accepted (rc=%d, err=%q)\n' "$v" "$rc" "$err"
  fi
done
setopt err_exit

# Test 82: __av1ify_is_nonneg_num — 負値や非数値は拒否
printf '\n## Test 82: __av1ify_is_nonneg_num rejects invalid values\n'
unsetopt err_exit
for v in -1 -0.5 abc 1.2.3 '' '+' '.'; do
  err=$(__av1ify_is_nonneg_num "$v" 2>&1)
  rc=$?
  if (( rc != 0 )) && [[ -z "$err" ]]; then
    printf '✓ "%s" is rejected (no stderr noise)\n' "$v"
  else
    printf '✗ "%s" should be rejected silently (rc=%d, err=%q)\n' "$v" "$rc" "$err"
  fi
done
setopt err_exit

# Test 83: 回帰防止 — postcheck 実行時に regex エラーが stderr に漏れない
# AV1IFY_SYNC_TOLERANCE に正常値を渡した上で、エンコード時のログに
# "repetition-operator operand invalid" が出ないことを確認
printf '\n## Test 83: No regex error leaks to stderr during encode\n'
TEST_DIR="$TEST_TMP/test83"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(AV1IFY_SYNC_TOLERANCE=0.5 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "repetition-operator" "No BSD ERE regex error in encode log"
assert_not_contains "$output" "operand invalid" "No regex 'operand invalid' message"

# Test 84: 先頭 `+` 付き threshold でも regex エラーが出ない
printf '\n## Test 84: Leading + threshold does not trigger regex error\n'
TEST_DIR="$TEST_TMP/test84"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(AV1IFY_SYNC_TOLERANCE=+0.5 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "repetition-operator" "No regex error with '+0.5' threshold"

# Test 85: 音声なしソース → 出力にも音声が無いのは正常 (noaudio NG にしない)
# 回帰防止: 旧実装は出力の音声有無だけを見ていたため、-an で正常エンコードした
# 音声なし素材が毎回 check_ng-noaudio にリネームされ、再実行のたび再エンコードされていた。
# 注: 音声なしソースは __video_health_check (チェック2) が破損扱いするため、
# このパスへは --force 経由でのみ到達する (health check の仕様は本テストのスコープ外)。
printf '\n## Test 85: Silent source (--force) - no noaudio false positive\n'
TEST_DIR="$TEST_TMP/test85"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_ACODEC= MOCK_AUDIO_INDEX= MOCK_OUTPUT_AUDIO_INDEX= av1ify --force "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "音声ストリーム検出できず" "No noaudio issue for silent source"
assert_not_contains "$output" "check_ng" "Silent source output is not marked check_ng"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output keeps normal -enc.mp4 name"

# Test 86: ソースに音声があるのに出力で消えた場合は従来どおり noaudio NG
printf '\n## Test 86: Audio lost in output - noaudio NG preserved\n'
TEST_DIR="$TEST_TMP/test86"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_OUTPUT_AUDIO_INDEX= av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "音声ストリーム検出できず" "Detects audio lost during encode"
assert_contains "$output" "check_ng" "Output is marked as check_ng"

# Test 87: ソースが参照できない場合は noaudio 判定をスキップせず NG side に倒す
# (Test 85 のスキップは「ソースの音声なしを確認できた」場合限定であることの裏面)
printf '\n## Test 87: Missing source at postcheck - noaudio NG preserved\n'
TEST_DIR="$TEST_TMP/test87"
mkdir -p "$TEST_DIR"
echo "encoded data" > "$TEST_DIR/video-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_OUTPUT_AUDIO_INDEX= __av1ify_postcheck "$TEST_DIR/video-enc.mp4" "$TEST_DIR/ghost.avi" 0 "" 2>&1)
rc=$?
setopt err_exit
assert_contains "$output" "音声ストリーム検出できず" "Reports noaudio when source is missing"
(( rc != 0 )) && printf '✓ postcheck returns non-zero (rc=%d)\n' "$rc" || { printf '✗ postcheck should return non-zero\n'; exit 1; }
assert_file_exists "$TEST_DIR/video-check_ng-noaudio-enc.mp4" "Output is renamed with noaudio tag"

# Test 88: ソースの音声 probe (ffprobe) が失敗した場合も NG side に倒す (codex P2 回帰防止)
# probe 失敗 (exit 非0) を「音声なしソース」と誤解釈すると、壊れたソースで
# 音声が消えた出力を黙って受理してしまう
printf '\n## Test 88: Source audio probe failure - noaudio NG preserved\n'
TEST_DIR="$TEST_TMP/test88"
mkdir -p "$TEST_DIR"
echo "source data" > "$TEST_DIR/input.avi"
echo "encoded data" > "$TEST_DIR/input-enc.mp4"

# stream=index のみ「出力=空成功 / ソース=exit 1」を返し、他は通常モックへ委譲する wrapper
PROBE_FAIL_BIN="$TEST_DIR/probe_fail_bin"
mkdir -p "$PROBE_FAIL_BIN"
cat > "$PROBE_FAIL_BIN/ffprobe" <<MOCKEOF
#!/usr/bin/env sh
last=""
for a in "\$@"; do last="\$a"; done
case "\$*" in
  *"stream=index"*)
    case "\$last" in
      *-enc*|*check_ng*) exit 0 ;;
      *) exit 1 ;;
    esac ;;
  *) exec "$MOCK_BIN_DIR/ffprobe" "\$@" ;;
esac
MOCKEOF
chmod +x "$PROBE_FAIL_BIN/ffprobe"
cd "$TEST_DIR"
unsetopt err_exit
__saved_path="$PATH"
PATH="$PROBE_FAIL_BIN:$PATH"
output=$(__av1ify_postcheck "$TEST_DIR/input-enc.mp4" "$TEST_DIR/input.avi" 0 "" 2>&1)
rc=$?
PATH="$__saved_path"
unset __saved_path
setopt err_exit
assert_contains "$output" "音声ストリーム検出できず" "Probe failure is not treated as silent source"
assert_not_contains "$output" "noaudio 判定をスキップ" "Skip path is not taken on probe failure"
(( rc != 0 )) && printf '✓ postcheck returns non-zero (rc=%d)\n' "$rc" || { printf '✗ postcheck should return non-zero\n'; exit 1; }

# Test 89: mark_issue のリネーム先が既存でも無言上書きしない (連番で衝突回避)
# 再実行で同名 check_ng が再生成されるケースで、前回の成果物を mv -f で潰さないこと
printf '\n## Test 89: mark_issue collision - no silent overwrite\n'
TEST_DIR="$TEST_TMP/test89"
mkdir -p "$TEST_DIR"
echo "new output" > "$TEST_DIR/input-enc.mp4"
echo "previous artifact" > "$TEST_DIR/input-check_ng-enc.mp4"
unsetopt err_exit
__av1ify_mark_issue "$TEST_DIR/input-enc.mp4" "check_ng"
rc=$?
marked="$REPLY"
setopt err_exit
if [[ "$marked" == "$TEST_DIR/input-check_ng2-enc.mp4" && -f "$marked" ]]; then
  printf '✓ Collision avoided with numbered note (%s)\n' "${marked:t}"
else
  printf '✗ Expected input-check_ng2-enc.mp4, got: %s\n' "$marked"; exit 1
fi
if [[ "$(cat "$TEST_DIR/input-check_ng-enc.mp4")" == "previous artifact" ]]; then
  printf '✓ Previous artifact preserved\n'
else
  printf '✗ Previous artifact was overwritten\n'; exit 1
fi
(( rc == 0 )) || { printf '✗ mark_issue should return 0\n'; exit 1; }

# Test 90: finalize は出力が生成されない (mv 失敗 = 割り込みで tmp が消された窓など) 場合、
# 元ファイルを絶対に削除しない。無音ソースだと postcheck が欠落出力に対し「NG なし」で
# success を返し、直後の削除ブロックが元を trash/rm するデータ損失経路の防止。
printf '\n## Test 90: finalize preserves source when output is missing (data-loss guard)\n'
TEST_DIR="$TEST_TMP/test90"
mkdir -p "$TEST_DIR"
echo "keep me" > "$TEST_DIR/keep.mp4"
__AV1IFY_DELETE_ORIGIN=1
unsetopt err_exit
# tmp が存在しない → mv 失敗 → final_out 未生成 → ガードが削除を阻止し return 1
__av1ify_finalize "$TEST_DIR/nope_tmp.mp4" "$TEST_DIR/out.av1.mp4" "$TEST_DIR/keep.mp4" "" "" >/dev/null 2>&1
rc=$?
setopt err_exit
__AV1IFY_DELETE_ORIGIN=0
assert_file_exists "$TEST_DIR/keep.mp4" "Source preserved when output not generated"
assert_file_not_exists "$TEST_DIR/out.av1.mp4" "No bogus output left behind"
(( rc == 1 )) || { printf '✗ finalize should return 1 (NG) on missing output\n'; exit 1; }

printf '\n=== Postcheck Tests Completed ===\n'
