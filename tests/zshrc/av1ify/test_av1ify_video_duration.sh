#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify 映像ストリーム尺の直接比較テスト (vidloss)
#
# 検証対象:
#   - avsync (音声-映像の相対差) や duration (コンテナ全体の長さ) では、音声が
#     満尺のまま映像だけが途中で打ち切られたケースを見逃しうる (前者は音声側の
#     状態に依存し、後者は音声に引きずられて総尺が正常に見える)。
#   - この検査は音声を一切参照せず、映像ストリームの尺だけをソースと直接比較する。
#   - デフォルト閾値 2.0s、AV1IFY_VIDEO_LOSS_TOLERANCE で上書き可能。
#   - ソース側 stream=duration が N/A (MKV 等) でも packet PTS にフォールバックする。
#   - ソースの真の duration が全パス取得不能なら判定スキップ (誤検知回避、他の
#     A/V チェックと同じ fail-open 方針)。
#
# 🚨 TEST_DIR の命名に "vidloss" のような検査対象タグの文字列を含めないこと。
# av1ify の出力にはファイルの絶対パスがそのまま出るため、ディレクトリ名に
# タグ文字列が入っていると assert_contains/assert_not_contains がパス文字列に
# 誤ってマッチし、タグが実際に付いたかどうかに関係なく常に「一致」してしまう
# (このファイルの初版で実際に踏んだ: vidloss_t3/t5/t7 のパス名だけで
# assert_not_contains "vidloss" が常に失敗していた)。

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Video-Duration (vidloss) Postcheck Tests ===\n\n'

# ----------------------------------------------------------------------
# Test 1: 実際の事故の再現 — 音声は満尺のまま映像だけ途中で打ち切られた
# (別マシンでのエンコードが 1:14:14 で映像だけ止まり、音声は 3:19:27 のまま
#  残っていた実例の値。format duration は音声に引きずられてソースとほぼ一致し、
#  duration チェックだけでは検知できない)
# ----------------------------------------------------------------------
printf '## Test 1: video truncated while audio stays full length (real incident values)\n'
TEST_DIR="$TEST_TMP/vd_t1"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_VIDEO_DURATION=11967.305 MOCK_AUDIO_DURATION=11967.305 \
         MOCK_OUTPUT_VIDEO_DURATION=4454.833 MOCK_OUTPUT_AUDIO_DURATION=11967.097 \
         MOCK_FORMAT_DURATION=11967.305 MOCK_OUTPUT_FORMAT_DURATION=11967.167 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "vidloss" "vidloss tag fires when video track is truncated"
assert_contains "$output" "映像ストリーム尺不一致" "vidloss warning message printed"
assert_contains "$output" "check_ng" "Output is marked as check_ng"

# ----------------------------------------------------------------------
# Test 2: 音声なしソース (--force 経由) でも映像の打ち切りを検知する
# avsync は音声が無いと丸ごとスキップされるが、この検査は音声を参照しないため
# 独立して機能する (audio が絡む機構と完全に切り離されていることの確認)。
# ----------------------------------------------------------------------
printf '\n## Test 2: silent source (no audio at all) still detects video truncation\n'
TEST_DIR="$TEST_TMP/vd_t2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_ACODEC= MOCK_AUDIO_INDEX= MOCK_OUTPUT_AUDIO_INDEX= \
         MOCK_VIDEO_DURATION=100.0 MOCK_OUTPUT_VIDEO_DURATION=40.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=40.0 \
         av1ify --force "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "vidloss" "vidloss tag fires for audio-less source too"
assert_not_contains "$output" "音声ストリーム検出できず" "no spurious noaudio issue for legit silent source"

# ----------------------------------------------------------------------
# Test 3: 差分が閾値内なら警告なし
# ----------------------------------------------------------------------
printf '\n## Test 3: video duration diff within tolerance - no warning\n'
TEST_DIR="$TEST_TMP/vd_t3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# Δ=0.5s < 2.0s(デフォルト閾値)
output=$(MOCK_VIDEO_DURATION=100.0 MOCK_OUTPUT_VIDEO_DURATION=99.5 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"vidloss"* ]]; then
  printf '✓ No vidloss warning within tolerance\n'
else
  bad '✗ Should not warn when video duration difference is within tolerance\n'
fi

# ----------------------------------------------------------------------
# Test 4: AV1IFY_VIDEO_LOSS_TOLERANCE で閾値をカスタマイズ (タイトに)
# ----------------------------------------------------------------------
printf '\n## Test 4: custom tolerance via AV1IFY_VIDEO_LOSS_TOLERANCE detects smaller gap\n'
TEST_DIR="$TEST_TMP/vd_t4"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# Δ=1.5s, デフォルト(2.0s)では通るが閾値を1.0sに下げると検出
output=$(AV1IFY_VIDEO_LOSS_TOLERANCE=1.0 \
         MOCK_VIDEO_DURATION=100.0 MOCK_OUTPUT_VIDEO_DURATION=98.5 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "vidloss" "custom tolerance detects smaller video-duration gap"

# ----------------------------------------------------------------------
# Test 5: AV1IFY_VIDEO_LOSS_TOLERANCE を緩めると通す
# ----------------------------------------------------------------------
printf '\n## Test 5: looser AV1IFY_VIDEO_LOSS_TOLERANCE lets larger gap pass\n'
TEST_DIR="$TEST_TMP/vd_t5"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(AV1IFY_VIDEO_LOSS_TOLERANCE=10.0 \
         MOCK_VIDEO_DURATION=100.0 MOCK_OUTPUT_VIDEO_DURATION=95.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=100.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"vidloss"* ]]; then
  printf '✓ No vidloss warning when tolerance is loosened\n'
else
  bad '✗ Should not warn when diff is within the loosened tolerance\n'
fi

# ----------------------------------------------------------------------
# Test 6: ソース側 stream=duration が N/A (MKV 等) → packet PTS にフォールバック
# ----------------------------------------------------------------------
printf '\n## Test 6: source stream=duration N/A -> falls back to packet PTS\n'
TEST_DIR="$TEST_TMP/vd_t6"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# src の stream=duration は N/A、packet PTS 実測で 100.0 が取れる。出力は 40.0 に打ち切り。
output=$(MOCK_VIDEO_DURATION="N/A" MOCK_FORMAT_DURATION=100.0 MOCK_VIDEO_LAST_PTS=100.0 \
         MOCK_OUTPUT_VIDEO_DURATION=40.0 MOCK_OUTPUT_FORMAT_DURATION=40.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "vidloss" "packet PTS fallback enables the check on MKV-like sources"

# ----------------------------------------------------------------------
# Test 7: ソースの真の duration が全パス取得不能 → 判定スキップ (誤検知回避)
# ----------------------------------------------------------------------
printf '\n## Test 7: source video duration fully unmeasurable -> check skipped\n'
TEST_DIR="$TEST_TMP/vd_t7"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_VIDEO_DURATION="N/A" MOCK_FORMAT_DURATION="N/A" MOCK_VIDEO_LAST_PTS="N/A" \
         MOCK_OUTPUT_VIDEO_DURATION=40.0 MOCK_OUTPUT_FORMAT_DURATION="N/A" \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "vidloss" "unmeasurable source duration is skipped (no false positive)"

# ----------------------------------------------------------------------
# Test 8: ソース由来の A/V mismatch を忠実に保存しただけでは vidloss は出ない
# (avsync Test 1 と同じ素材: 映像の尺自体は encode 前後でほぼ不変)
# ----------------------------------------------------------------------
printf '\n## Test 8: source-induced A/V mismatch alone does not trigger vidloss\n'
TEST_DIR="$TEST_TMP/vd_t8"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_VIDEO_DURATION=8327.16 MOCK_AUDIO_DURATION=8309.31 \
         MOCK_OUTPUT_VIDEO_DURATION=8327.19 MOCK_OUTPUT_AUDIO_DURATION=8309.35 \
         MOCK_FORMAT_DURATION=8327.16 MOCK_OUTPUT_FORMAT_DURATION=8327.19 \
         MOCK_NB_FRAMES=249566 MOCK_OUTPUT_NB_FRAMES=249566 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "vidloss" "video duration preserved end-to-end -> no vidloss"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output renamed without check_ng tag"

# ----------------------------------------------------------------------
# Test 9: ソースの宣言 duration が嘘 (mdhd/tkhd がサンプルテーブルと不一致) でも
# packet 実測での再判定により誤検知しない (avsync Test 14 の直接比較版)
#
# 回帰防止: 初版は __av1ify_get_stream_end の cheap path (宣言 duration) を
# そのまま比較に使い、閾値超過時の packet 再測定を持たなかったため、この
# シナリオで false positive を出していた (avsync スイート実行で発覚)。
# ----------------------------------------------------------------------
printf '\n## Test 9: lying source stream=duration -> re-measured by packet PTS, no false vidloss\n'
TEST_DIR="$TEST_TMP/vd_t9"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# src: 宣言 v:0 duration=8270.837 (嘘) だが packet 実測は 8287.479。
# out: 宣言 v:0 duration=8287.513、packet 実測も 8287.479 で src の実測とほぼ一致。
# 宣言値どうしの差は 16.676s (閾値超過) だが、実測どうしの差は 0.034s (閾値内) なので
# 正常と判定されるべき。
output=$(MOCK_VIDEO_DURATION=8270.837 MOCK_VIDEO_LAST_PTS=8287.479 \
         MOCK_OUTPUT_VIDEO_DURATION=8287.513 MOCK_OUTPUT_VIDEO_LAST_PTS=8287.479 \
         MOCK_FORMAT_DURATION=8287.552 MOCK_OUTPUT_FORMAT_DURATION=8287.552 \
         MOCK_NB_FRAMES=248377 MOCK_OUTPUT_NB_FRAMES=248377 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_not_contains "$output" "vidloss" "lying declared duration does not trigger vidloss after re-measurement"
assert_contains "$output" "packet 実測" "re-measurement is reported to the user"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output renamed cleanly (no check_ng)"

# ----------------------------------------------------------------------
# Test 10: 宣言・実測とも欠落を示す本物の映像喪失は再判定後も検出される
# (Test 9 の緩和が false negative を作っていないことの確認)
# ----------------------------------------------------------------------
printf '\n## Test 10: genuine video loss survives packet re-measurement\n'
TEST_DIR="$TEST_TMP/vd_t10"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# src: 宣言も実測も 100.0 / out: 宣言も実測も 40.0 (実測しても差 60s は残る)
output=$(MOCK_VIDEO_DURATION=100.0 MOCK_VIDEO_LAST_PTS=100.0 \
         MOCK_OUTPUT_VIDEO_DURATION=40.0 MOCK_OUTPUT_VIDEO_LAST_PTS=40.0 \
         MOCK_FORMAT_DURATION=100.0 MOCK_OUTPUT_FORMAT_DURATION=40.0 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "vidloss" "genuine 60s video loss still flagged after re-measurement"

printf '\n=== Video-Duration (vidloss) Postcheck Tests Completed ===\n'
