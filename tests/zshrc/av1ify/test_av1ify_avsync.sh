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
# (本件の動機ケース: 元 MKV で音声が映像より 17.85s 短く、
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
# 実装は presentation end を %.6f で整形して返すため値の一致で比較する (書式では比較しない)
if (( rc == 0 )) && (( REPLY == 99.85 )); then
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

# ----------------------------------------------------------------------
# Test 14: ソースの宣言 duration が嘘 (mdhd/tkhd がサンプルテーブルと不一致)
# → packet 実測での再判定により誤検知しない
#
# 実例: ソース mp4 の video stream=duration が 8270.837s と宣言されているが、
# 実際の packet 末尾は 8287.479s (500 フレーム分の嘘)。encode 出力は正しい値を書くため、
# 宣言ベースだと Δ=16.68s の「音ズレ」に見える。実測ベースでは Δ=0.013s で正常。
# ----------------------------------------------------------------------
printf '\n## Test 14: lying source stream=duration -> re-measured by packet PTS, no false avsync\n'
TEST_DIR="$TEST_TMP/avs_t14"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_VIDEO_DURATION=8270.837 MOCK_AUDIO_DURATION=8287.552 \
         MOCK_VIDEO_LAST_PTS=8287.479 MOCK_AUDIO_LAST_PTS=8287.531 \
         MOCK_OUTPUT_VIDEO_DURATION=8287.513 MOCK_OUTPUT_AUDIO_DURATION=8287.552 \
         MOCK_OUTPUT_VIDEO_LAST_PTS=8287.479 MOCK_OUTPUT_AUDIO_LAST_PTS=8287.531 \
         MOCK_FORMAT_DURATION=8287.552 MOCK_OUTPUT_FORMAT_DURATION=8287.552 \
         MOCK_NB_FRAMES=248377 MOCK_OUTPUT_NB_FRAMES=248377 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "avsync" "lying declared duration does not trigger avsync"
assert_contains "$output" "packet 実測" "re-measurement is reported to the user"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output renamed cleanly (no check_ng)"

# ----------------------------------------------------------------------
# Test 15: 宣言・実測とも drift を示す本物のズレは再判定後も検出される
# (Test 14 の緩和が false negative を作っていないことの確認)
# ----------------------------------------------------------------------
printf '\n## Test 15: genuine drift survives packet re-measurement\n'
TEST_DIR="$TEST_TMP/avs_t15"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# src: gap=0 (宣言も実測も) / out: audio が 3s 長い (宣言も実測も) → 再判定しても drift=3
output=$(MOCK_VIDEO_DURATION=100.0 MOCK_AUDIO_DURATION=100.0 \
         MOCK_VIDEO_LAST_PTS=100.0 MOCK_AUDIO_LAST_PTS=100.0 \
         MOCK_OUTPUT_VIDEO_DURATION=100.0 MOCK_OUTPUT_AUDIO_DURATION=103.0 \
         MOCK_OUTPUT_VIDEO_LAST_PTS=100.0 MOCK_OUTPUT_AUDIO_LAST_PTS=103.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "avsync" "genuine 3s drift still flagged after re-measurement"

# ----------------------------------------------------------------------
# Test 16: __av1ify_packet_end は B-frame の reorder に耐える
# packet はデコード順で出るため最終行が最大 PTS とは限らない。表示終端は
# max(pts_time + duration_time) で測る必要がある (最終行を使うと過小評価し、
# drift を縮めて本物の音ズレを見逃す)。
# 実測の裏付け: x264 bframes=16 / 1fps の 20s ソースで最終行 18.0s / 実際 20.0s。
# ----------------------------------------------------------------------
printf '\n## Test 16: __av1ify_packet_end uses presentation end, not last packet line\n'
TEST_DIR="$TEST_TMP/avs_t16"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/x.avi"
REPLY=""
# デコード順: 17,19,18 (reorder)。表示終端は 19+1=20.0
MOCK_FORMAT_DURATION=20.0 MOCK_PACKET_LINES='17.000000,1.000000\n19.000000,1.000000\n18.000000,1.000000' \
  __av1ify_packet_end "$TEST_DIR/x.avi" "v:0"
rc=$?
if (( rc == 0 )) && [[ "$REPLY" == "20.000000" ]]; then
  printf '✓ presentation end from reordered packets (REPLY=%s)\n' "$REPLY"
else
  printf '✗ Expected 20.000000, got rc=%d REPLY=%q\n' "$rc" "$REPLY"
  exit 1
fi

# ----------------------------------------------------------------------
# Test 17: duration_time が N/A の行でも pts だけで最大値を取る
# ----------------------------------------------------------------------
printf '\n## Test 17: __av1ify_packet_end tolerates N/A duration_time\n'
TEST_DIR="$TEST_TMP/avs_t17"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/x.avi"
REPLY=""
MOCK_FORMAT_DURATION=20.0 MOCK_PACKET_LINES='17.000000,N/A\n19.500000,N/A\nN/A,N/A' \
  __av1ify_packet_end "$TEST_DIR/x.avi" "v:0"
rc=$?
if (( rc == 0 )) && [[ "$REPLY" == "19.500000" ]]; then
  printf '✓ falls back to pts when duration_time is N/A (REPLY=%s)\n' "$REPLY"
else
  printf '✗ Expected 19.500000, got rc=%d REPLY=%q\n' "$rc" "$REPLY"
  exit 1
fi

# ----------------------------------------------------------------------
# Test 18: 時間シフト型の音ズレ (終端は揃うが先頭がずれる) は再判定で降格されない
#
# issue 058 の再現 (値は -itsoffset 5 で作った実ファイルの ffprobe 実測):
# 音声の先頭 ~5s が落ちて edit-list 遅延 (start_time=4.976) が書かれ、表示終端は
# 映像と揃っている。宣言 duration ベースでは Δ=4.98s だが packet 実測の「終端」では
# Δ≈0 になるため、開始オフセットを見ない再判定はこれを正常へ降格していた
# (997d078 が NG にしていた実害級の出力が ✅ 完了で素通りするデグレ)。
# ----------------------------------------------------------------------
printf '\n## Test 18: time-shifted audio (aligned tail, shifted head) stays flagged\n'
TEST_DIR="$TEST_TMP/avs_t18"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_VIDEO_DURATION=20.0 MOCK_AUDIO_DURATION=20.0 \
         MOCK_VIDEO_LAST_PTS=20.0 MOCK_AUDIO_LAST_PTS=20.0 \
         MOCK_OUTPUT_VIDEO_DURATION=20.0 MOCK_OUTPUT_AUDIO_DURATION=15.023311 \
         MOCK_OUTPUT_VIDEO_LAST_PTS=20.0 MOCK_OUTPUT_AUDIO_LAST_PTS=19.999320 \
         MOCK_OUTPUT_AUDIO_START=4.976009 \
         MOCK_FORMAT_DURATION=20.0 MOCK_OUTPUT_FORMAT_DURATION=20.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "avsync" "time-shifted audio is still flagged (not downgraded by tail re-measurement)"
assert_contains "$output" "時間シフト型" "the reason (start-offset shift) is reported to the user"

printf '\n=== A/V Sync Postcheck Tests Completed ===\n'
