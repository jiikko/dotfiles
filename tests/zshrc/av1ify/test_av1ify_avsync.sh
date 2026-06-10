#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify A/V sync postcheck テスト
#
# 検証対象:
#   - encode 前後の A/V duration の関係差 (drift) のみで判定する relative ロジック
#   - ソース時点で A/V がズレている素材 (末尾無音映像 MKV 等) を encode 由来と
#     誤検出しない
#   - ソースが stream=duration を出さない (MKV) ケースで packet PTS スキャンに
#     フォールバックして relative 判定が機能する
#   - デフォルト閾値 2.0s
#   - AV1IFY_SYNC_TOLERANCE 環境変数で閾値上書き
#   - ソース不在/出力 duration 取得不能のときは判定スキップ (絶対値 fallback で
#     誤検出を起こさない)

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify A/V Sync Postcheck Tests ===\n\n'

# ----------------------------------------------------------------------
# Test 1: ソース由来 A/V mismatch + encode が忠実に保存 → avsync 警告は出ない
# (本件の動機ケース: MDUD-051 のように元 MKV で音声が映像より 17.85s 短く、
# encode 出力でも 17.83s ずれてるが drift は 0.02s で実害なし)
# ----------------------------------------------------------------------
printf '## Test 1: source-induced A/V mismatch faithfully preserved -> no avsync\n'
TEST_DIR="$TEST_TMP/avs_t1"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_VIDEO_DURATION=8327.16 MOCK_AUDIO_DURATION=8309.31 \
         MOCK_OUTPUT_VIDEO_DURATION=8327.19 MOCK_OUTPUT_AUDIO_DURATION=8309.35 \
         MOCK_FORMAT_DURATION=8327.16 MOCK_OUTPUT_FORMAT_DURATION=8327.19 \
         MOCK_NB_FRAMES=249566 MOCK_OUTPUT_NB_FRAMES=249566 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "avsync" "no avsync tag when source-induced gap preserved"
assert_not_contains "$output" "音ズレ疑い" "no avsync warning message"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output renamed without check_ng tag"

# ----------------------------------------------------------------------
# Test 2: encode が新たに 3s 広げた → avsync 警告が出る (閾値 2.0 超過)
# ----------------------------------------------------------------------
printf '\n## Test 2: encode-introduced 3s drift -> avsync flagged\n'
TEST_DIR="$TEST_TMP/avs_t2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# src: gap=0  /  out: gap=3 (audio 3s 長くなった) → drift=3, threshold 2.0 超え
output=$(MOCK_VIDEO_DURATION=100.0 MOCK_AUDIO_DURATION=100.0 \
         MOCK_OUTPUT_VIDEO_DURATION=100.0 MOCK_OUTPUT_AUDIO_DURATION=103.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "avsync" "avsync tag for 3s encode-introduced drift"
assert_contains "$output" "音ズレ疑い" "avsync warning printed"
assert_file_exists "$TEST_DIR/input-check_ng-avsync-enc.mp4" "Output renamed with check_ng-avsync tag"

# ----------------------------------------------------------------------
# Test 3: encode が 1s だけ広げた → 閾値 2.0 内なので警告なし
# ----------------------------------------------------------------------
printf '\n## Test 3: encode-introduced 1s drift -> within 2.0s default tolerance\n'
TEST_DIR="$TEST_TMP/avs_t3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_VIDEO_DURATION=100.0 MOCK_AUDIO_DURATION=100.0 \
         MOCK_OUTPUT_VIDEO_DURATION=100.0 MOCK_OUTPUT_AUDIO_DURATION=101.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "avsync" "no avsync tag for sub-threshold drift"

# ----------------------------------------------------------------------
# Test 4: AV1IFY_SYNC_TOLERANCE=0.5 で閾値タイトに → 1s drift が引っかかる
# ----------------------------------------------------------------------
printf '\n## Test 4: AV1IFY_SYNC_TOLERANCE=0.5 makes 1s drift fail\n'
TEST_DIR="$TEST_TMP/avs_t4"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(AV1IFY_SYNC_TOLERANCE=0.5 \
         MOCK_VIDEO_DURATION=100.0 MOCK_AUDIO_DURATION=100.0 \
         MOCK_OUTPUT_VIDEO_DURATION=100.0 MOCK_OUTPUT_AUDIO_DURATION=101.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "avsync" "tighter threshold triggers avsync on 1s drift"

# ----------------------------------------------------------------------
# Test 5: AV1IFY_SYNC_TOLERANCE=5.0 で閾値ゆるく → 3s drift をスルー
# ----------------------------------------------------------------------
printf '\n## Test 5: AV1IFY_SYNC_TOLERANCE=5.0 lets 3s drift pass\n'
TEST_DIR="$TEST_TMP/avs_t5"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(AV1IFY_SYNC_TOLERANCE=5.0 \
         MOCK_VIDEO_DURATION=100.0 MOCK_AUDIO_DURATION=100.0 \
         MOCK_OUTPUT_VIDEO_DURATION=100.0 MOCK_OUTPUT_AUDIO_DURATION=103.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "avsync" "looser threshold lets 3s drift pass"

# ----------------------------------------------------------------------
# Test 6: ソース stream=duration が N/A (MKV) でも packet PTS にフォールバック
# 元動画は 0.02s drift だけ持つ素材を想定。relative 判定が走り、警告なしになる。
# ----------------------------------------------------------------------
printf '\n## Test 6: source stream=duration N/A -> packet PTS fallback works\n'
TEST_DIR="$TEST_TMP/avs_t6"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# src stream=duration は N/A、packet=pts_time から取れる → src_v=8327.16, src_a=8309.31
# out stream=duration は通常通り取れる
output=$(MOCK_VIDEO_DURATION="N/A" MOCK_AUDIO_DURATION="N/A" \
         MOCK_VIDEO_LAST_PTS=8327.16 MOCK_AUDIO_LAST_PTS=8309.31 \
         MOCK_OUTPUT_VIDEO_DURATION=8327.19 MOCK_OUTPUT_AUDIO_DURATION=8309.35 \
         MOCK_FORMAT_DURATION=8327.16 MOCK_OUTPUT_FORMAT_DURATION=8327.19 \
         MOCK_NB_FRAMES=249566 MOCK_OUTPUT_NB_FRAMES=249566 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "avsync" "packet PTS fallback enables relative judgment"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output renamed cleanly (no check_ng)"

# ----------------------------------------------------------------------
# Test 7: ソース duration が全パス取得不能 → avsync 判定はスキップ (誤検出回避)
# 旧バージョンは絶対値 fallback で誤検出していたが、新バージョンは敢えてスキップ。
# ----------------------------------------------------------------------
printf '\n## Test 7: all source duration paths fail -> avsync judgment skipped\n'
TEST_DIR="$TEST_TMP/avs_t7"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# src の stream=duration も packet=pts_time も N/A → relative 不能
# out は絶対 gap 17.8s だが、旧 logic だと絶対値で誤発火、新 logic はスキップ
output=$(MOCK_VIDEO_DURATION="N/A" MOCK_AUDIO_DURATION="N/A" \
         MOCK_VIDEO_LAST_PTS="N/A" MOCK_AUDIO_LAST_PTS="N/A" \
         MOCK_OUTPUT_VIDEO_DURATION=8327.19 MOCK_OUTPUT_AUDIO_DURATION=8309.35 \
         MOCK_FORMAT_DURATION="N/A" MOCK_OUTPUT_FORMAT_DURATION=8327.19 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "avsync" "absolute fallback is removed (no false-positive)"

# ----------------------------------------------------------------------
# Test 8: 出力の方向反転 (encode で audio/video の長短が逆転) → 警告される
# src: audio 短い (gap=-1)、out: audio 長い (gap=+5) → drift=6, threshold 超え
# ----------------------------------------------------------------------
printf '\n## Test 8: A/V relationship inversion is detected\n'
TEST_DIR="$TEST_TMP/avs_t8"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_VIDEO_DURATION=100.0 MOCK_AUDIO_DURATION=99.0 \
         MOCK_OUTPUT_VIDEO_DURATION=100.0 MOCK_OUTPUT_AUDIO_DURATION=105.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "avsync" "inversion (src gap=-1 -> out gap=+5, drift=6) flagged"

# ----------------------------------------------------------------------
# Test 9: 警告メッセージに threshold が含まれる (デバッグ容易性)
# ----------------------------------------------------------------------
printf '\n## Test 9: avsync warning includes threshold value\n'
TEST_DIR="$TEST_TMP/avs_t9"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(AV1IFY_SYNC_TOLERANCE=1.0 \
         MOCK_VIDEO_DURATION=100.0 MOCK_AUDIO_DURATION=100.0 \
         MOCK_OUTPUT_VIDEO_DURATION=100.0 MOCK_OUTPUT_AUDIO_DURATION=103.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "threshold=1.0s" "warning message exposes threshold"

# ----------------------------------------------------------------------
# Test 10: __av1ify_get_stream_end 単体: stream=duration が数値なら即返す
# ----------------------------------------------------------------------
printf '\n## Test 10: __av1ify_get_stream_end uses cheap path when stream=duration is numeric\n'
TEST_DIR="$TEST_TMP/avs_t10"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/x.avi"
REPLY=""
MOCK_VIDEO_DURATION=42.5 __av1ify_get_stream_end "$TEST_DIR/x.avi" "v:0"
rc=$?
if (( rc == 0 )) && [[ "$REPLY" == "42.5" ]]; then
  printf '✓ cheap path returns stream=duration value (REPLY=%s)\n' "$REPLY"
else
  printf '✗ cheap path failed (rc=%d, REPLY=%q)\n' "$rc" "$REPLY"
fi

# ----------------------------------------------------------------------
# Test 11: __av1ify_get_stream_end 単体: stream=duration が N/A なら packet PTS から取る
# ----------------------------------------------------------------------
printf '\n## Test 11: __av1ify_get_stream_end falls back to packet PTS when stream=duration N/A\n'
TEST_DIR="$TEST_TMP/avs_t11"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/x.avi"
REPLY=""
MOCK_VIDEO_DURATION="N/A" MOCK_FORMAT_DURATION=100.0 MOCK_VIDEO_LAST_PTS=99.85 \
  __av1ify_get_stream_end "$TEST_DIR/x.avi" "v:0"
rc=$?
if (( rc == 0 )) && [[ "$REPLY" == "99.85" ]]; then
  printf '✓ fallback path returns packet PTS value (REPLY=%s)\n' "$REPLY"
else
  printf '✗ fallback path failed (rc=%d, REPLY=%q)\n' "$rc" "$REPLY"
fi

# ----------------------------------------------------------------------
# Test 12: __av1ify_get_stream_end 単体: 全パス N/A なら失敗
# ----------------------------------------------------------------------
printf '\n## Test 12: __av1ify_get_stream_end returns failure when all paths N/A\n'
TEST_DIR="$TEST_TMP/avs_t12"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/x.avi"
REPLY="sentinel"
unsetopt err_exit
MOCK_VIDEO_DURATION="N/A" MOCK_FORMAT_DURATION="N/A" MOCK_VIDEO_LAST_PTS="N/A" \
  __av1ify_get_stream_end "$TEST_DIR/x.avi" "v:0"
rc=$?
setopt err_exit
if (( rc == 1 )) && [[ -z "$REPLY" ]]; then
  printf '✓ all-N/A returns failure with REPLY empty (rc=%d)\n' "$rc"
else
  printf '✗ Expected rc=1 and REPLY="", got rc=%d REPLY=%q\n' "$rc" "$REPLY"
fi

# ----------------------------------------------------------------------
# Test 13: 不正な AV1IFY_SYNC_TOLERANCE はデフォルト 2.0 にフォールバック
# (旧テスト 83 の正常値路線を新デフォルトで再検証)
# ----------------------------------------------------------------------
printf '\n## Test 13: invalid AV1IFY_SYNC_TOLERANCE falls back to default 2.0\n'
TEST_DIR="$TEST_TMP/avs_t13"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# 1.5s drift: デフォルト 2.0 なら通る、誤値が 0 にされたら引っかかる
output=$(AV1IFY_SYNC_TOLERANCE="bogus" \
         MOCK_VIDEO_DURATION=100.0 MOCK_AUDIO_DURATION=100.0 \
         MOCK_OUTPUT_VIDEO_DURATION=100.0 MOCK_OUTPUT_AUDIO_DURATION=101.5 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "avsync" "invalid threshold falls back to 2.0 (1.5s drift passes)"

printf '\n=== A/V Sync Postcheck Tests Completed ===\n'
