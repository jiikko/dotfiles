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
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# ソース=10.0s, 出力=9.5s → Δ=0.5s < 2.0s で正常
output=$(MOCK_FORMAT_DURATION=10.0 MOCK_OUTPUT_FORMAT_DURATION=9.5 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"再生時間ズレ"* ]]; then
  printf '✓ No duration warning within tolerance\n'
else
  bad '✗ Should not warn when duration difference is within tolerance\n'
fi

# Test 67: AV1IFY_DURATION_TOLERANCE で閾値をカスタマイズ
printf '\n## Test 67: Custom duration tolerance via AV1IFY_DURATION_TOLERANCE\n'
TEST_DIR="$TEST_TMP/test67"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=300 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム数不一致"* ]]; then
  printf '✓ No frame count warning when counts match\n'
else
  bad '✗ Should not warn when frame counts match\n'
fi

# Test 69b: フレーム数差が閾値内（Δ≤24）なら警告なし
printf '\n## Test 69b: Frame count difference within tolerance - no warning\n'
TEST_DIR="$TEST_TMP/test69b"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# ソース=300フレーム, 出力=285フレーム → Δ=15 ≤ 24(デフォルト閾値)で正常
output=$(MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=285 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム数不一致"* ]]; then
  printf '✓ No frame count warning when difference is within tolerance\n'
else
  bad '✗ Should not warn when frame count difference ≤ 24\n'
fi

# Test 69c: AV1IFY_FRAME_TOLERANCE で閾値をカスタマイズ
printf '\n## Test 69c: Custom frame tolerance via AV1IFY_FRAME_TOLERANCE\n'
TEST_DIR="$TEST_TMP/test69c"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# ソース=45000フレーム (25分@30fps 相当), Δ=88。絶対フロア 24 は超えるが
# 相対許容 45000*0.5%=225 の範囲内 → 警告なし (2026-07-12 の緩和の回帰テスト)
output=$(MOCK_NB_FRAMES=45000 MOCK_OUTPUT_NB_FRAMES=44912 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム数不一致"* ]]; then
  printf '✓ No frame count warning when diff is within relative tolerance\n'
else
  bad '✗ Should not warn when diff ≤ 0.5%% of source frames\n'
fi

# Test 69e: AV1IFY_FRAME_TOLERANCE_PCT で相対許容をカスタマイズ
printf '\n## Test 69e: Custom relative tolerance via AV1IFY_FRAME_TOLERANCE_PCT\n'
TEST_DIR="$TEST_TMP/test69e"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# fps変更あり(60→30)の場合、フレーム数が異なっても警告しない
output=$(MOCK_FPS="60000/1001" MOCK_NB_FRAMES=600 MOCK_OUTPUT_NB_FRAMES=300 av1ify --fps 30 "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム数不一致"* ]]; then
  printf '✓ No frame count warning when fps changed\n'
else
  bad '✗ Should not check frame count when fps is changed\n'
fi

# Test 70b: VFR ソース検出時は「ソースとのフレーム数比較」をやめる (issue 394)。
# ただし検査を捨てるのではなく密度検査へ切り替わる (issue 397) ので、
# 密度が期待どおりのケースでは何の警告も出ないことを確認する。
# 🚨 期待値は **ソースの nb_frames** (適用fps を経由しない独立な量)。CFR 正規化は
# avg_frame_rate = nb_frames / duration の定義どおり、フレーム数を丸め誤差の範囲で保つ。
printf '\n## Test 70b: VFR source swaps the src-frame-count check for a density check (issue 394/397)\n'
TEST_DIR="$TEST_TMP/test70b"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# 正しい正規化: 出力のフレーム数はソースとほぼ同じ (丸めで 1 枚ぶれる)
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="29970029/1000000" MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=301 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム数不一致"* ]]; then
  printf '✓ No src-frame-count warning when VFR source is detected\n'
else
  bad '✗ Should not compare against the source frame count for VFR sources\n'
fi
if [[ "$output" != *"フレーム密度不一致"* ]]; then
  printf '✓ No density warning when the output density matches the applied fps\n'
else
  bad '✗ Density check must not fire when the density is correct\n%s\n' "$output"
fi

# Test 70d: VFR 検出時でも、密度が壊れていれば NG になる (issue 397 の中心)
# 🚨 ここが無いと「-r に誤った値が渡ってフレームの大半が失われる」故障を誰も検出できない。
# 実測 (実 ffmpeg): 30fps/10s/300 フレームを -r 1 で作り直すと 12 フレームになるが、
# duration の差はちょうど 2.000s で閾値を超えず、元ファイルが削除されていた。
printf '\n## Test 70d: Broken density is still NG for VFR sources (issue 397)\n'
TEST_DIR="$TEST_TMP/test70d"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# 期待密度 ≈ 300 に対し出力は 12 フレーム (96% 欠落)
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="29970029/1000000" MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=12 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" == *"フレーム密度不一致"* ]]; then
  printf '✓ Density check fires when most frames are missing\n'
else
  bad '✗ Density check did not fire (96%% of frames lost went undetected)\n%s\n' "$output"
fi
if [[ -f "$TEST_DIR/input-check_ng-density-enc.mp4" ]]; then
  printf '✓ The output is renamed to check_ng-density\n'
else
  bad '✗ The output was not marked check_ng-density: %s\n' "$(ls "$TEST_DIR")"
fi
if [[ -f "$TEST_DIR/input.avi" ]]; then
  printf '✓ The source file is kept (not deleted) when the density is broken\n'
else
  bad '✗ The source file was deleted despite the broken density\n'
fi

# Test 70e: 音声が映像より長いソースで密度検査が誤爆しない (issue 397 の 2 周目 P1-2)
# 🚨 期待値に format=duration (= 全ストリームの最大) を使うと、末尾に音声が残る素材で
# 期待値が過大になり、**1 フレームも失っていない出力**が check_ng-density に転ぶ。
# 実測 2026-09-19: 映像 60s / 音声 64s の VFR mp4 で、親 commit では ✅ 完了 だったものが
# check_ng-density になった (ffmpeg のログは dup=1 drop=0 = フレームは無傷)。
printf '\n## Test 70e: Density check does not misfire when audio outlasts video (issue 397 r2)\n'
TEST_DIR="$TEST_TMP/test70e"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# コンテナ尺 64s (音声が長い) / 映像は 60s 相当。フレーム数はソースと出力で一致している
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="29970029/1000000" MOCK_FORMAT_DURATION=64.0 \
         MOCK_VIDEO_DURATION=60.0 MOCK_NB_FRAMES=1800 MOCK_OUTPUT_NB_FRAMES=1800 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム密度不一致"* ]]; then
  printf '✓ No density warning when the container duration exceeds the video duration\n'
else
  bad '✗ Density check misfired on an audio-longer-than-video source\n%s\n' "$output"
fi

# Test 70f: 密度検査のフロアは専用変数で、ユーザー向けの AV1IFY_FRAME_TOLERANCE では緩まない
# (issue 398 と同型の相乗りを、398 の兄弟修正が再生産していた。2 周目 P2)
printf '\n## Test 70f: AV1IFY_FRAME_TOLERANCE does not disable the density check (issue 397 r2)\n'
TEST_DIR="$TEST_TMP/test70f"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(AV1IFY_FRAME_TOLERANCE=100000 MOCK_FPS="29/1" MOCK_AVG_FPS="29970029/1000000" \
         MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=12 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" == *"フレーム密度不一致"* ]]; then
  printf '✓ Density check still fires when AV1IFY_FRAME_TOLERANCE is loosened\n'
else
  bad '✗ AV1IFY_FRAME_TOLERANCE silently disabled the density check\n%s\n' "$output"
fi

# Test 70g: nb_frames を出さないコンテナ (MKV / TS / FLV 等) のフォールバック経路
# (3 周目の指摘 P1-2: この約 20 行をテストが 1 本も守っていなかった。mock は必ず
#  nb_frames を正の整数で返すので primary 経路しか通らず、e2e の fixture も mp4 だった)
printf '\n## Test 70g: Fallback expectation for containers without nb_frames (issue 397 r3)\n'
# 🚨 拡張子は .mkv にする: 実 AVI は nb_frames を出すので、.avi に MOCK_NB_FRAMES="N/A" を
# 与えると実在しない組み合わせになり「nb_frames を出さないコンテナ」の代表として誤解を招く
TEST_DIR="$TEST_TMP/test70g"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.mkv"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# nb_frames は N/A。映像 10s × avg 29.970 ≈ 300 が期待値になり、出力 300 なので警告なし
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="29970029/1000000" MOCK_NB_FRAMES="N/A" \
         MOCK_VIDEO_DURATION=10.0 MOCK_OUTPUT_NB_FRAMES=300 av1ify "$TEST_DIR/input.mkv" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム密度不一致"* ]]; then
  printf '✓ Fallback accepts a correct density when nb_frames is unavailable\n'
else
  bad '✗ Fallback misfired on a correct encode\n%s\n' "$output"
fi

# 同じフォールバック経路で、密度が壊れていれば検出できること (上だけだと
# 「常に緑を返す実装」でも通ってしまう)
TEST_DIR="$TEST_TMP/test70g2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.mkv"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="29970029/1000000" MOCK_NB_FRAMES="N/A" \
         MOCK_VIDEO_DURATION=10.0 MOCK_OUTPUT_NB_FRAMES=12 av1ify "$TEST_DIR/input.mkv" 2>&1 || true)
setopt err_exit
if [[ "$output" == *"フレーム密度不一致"* ]]; then
  printf '✓ Fallback still detects a broken density\n'
else
  bad '✗ Fallback did not detect a broken density\n%s\n' "$output"
fi

# Test 70h: nb_frames が尺と矛盾するとき (edit list 等) は nb_frames を採用しない
# 🚨 mp4 の nb_frames は stsz のサンプル数。`ffmpeg -ss N -i x -c copy` が書く elst が
# あると切る前の値が残り、素朴に信じると正しいエンコードが check_ng-density に転ぶ。
printf '\n## Test 70h: A nb_frames inconsistent with the duration is rejected (issue 397 r3)\n'
TEST_DIR="$TEST_TMP/test70h"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# nb_frames=686 (切る前の値) だが、映像 15s × avg 34.3 ≈ 515 が実態。出力は 515
output=$(MOCK_FPS="40/1" MOCK_AVG_FPS="343/10" MOCK_NB_FRAMES=686 \
         MOCK_FORMAT_DURATION=15.0 MOCK_VIDEO_DURATION=15.0 MOCK_AUDIO_DURATION=15.0 \
         MOCK_OUTPUT_NB_FRAMES=515 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム密度不一致"* ]]; then
  printf '✓ A trimmed source is not falsely flagged\n'
else
  bad '✗ The stale nb_frames was trusted and the encode was flagged\n%s\n' "$output"
fi
if [[ "$output" == *"尺と矛盾する"* ]]; then
  printf '✓ The rejection of nb_frames is reported\n'
else
  bad '✗ The rejection of nb_frames was silent\n%s\n' "$output"
fi

# Test 70i: nb_frames が尺と整合していれば **nb_frames が期待値として使われる** (逆選択の pin)
#
# 🚨 「却下メッセージが出ないこと」で採用を観測しないこと。それは else 枝の print を
# 見ているだけで、採用枝を `want_frames="$want_by_duration"` に差し替える (= 常に尺ベースを
# 使う。ただし黙って) 変異を**緑のまま通す** (実測 2026-09-19 の敵対レビュー 5 周目)。
# 採用/非採用で**密度の判定そのものが割れる**入力を使う:
#   尺ベース=300 / nb=310 (差 10 ≤ trust 許容 12 なので採用される) / 出力=330
#   採用時: 期待値 310、Δ=20 ≤ 許容 24 → 警告なし
#   非採用: 期待値 300、Δ=30 > 許容 24 → 警告が出る
printf '\n## Test 70i: A consistent nb_frames is actually used as the expectation (issue 397 r5)\n'
TEST_DIR="$TEST_TMP/test70i"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="30/1" MOCK_NB_FRAMES=310 \
         MOCK_VIDEO_DURATION=10.0 MOCK_OUTPUT_NB_FRAMES=330 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム密度不一致"* ]]; then
  printf '✓ The nb_frames value (not the duration-based one) is used as the expectation\n'
else
  bad '✗ The duration-based expectation was used despite a consistent nb_frames\n%s\n' "$output"
fi
if [[ "$output" != *"尺と矛盾する"* ]]; then
  printf '✓ A consistent nb_frames is not rejected\n'
else
  bad '✗ A consistent nb_frames was rejected\n%s\n' "$output"
fi

# Test 70j: **採用された** nb の残差 + 丸めが密度許容を突き抜けない (崖が無いこと)
#
# 🚨 4 周目の版 (nb=324 / 尺ベース=300) は trust ゲートが却下する入力だったので、
# 主張していること (採用された非厳密な nb で密度検査が誤爆しない) を一度も実行していなかった
# (実体は trust 許容の定数が変わったことの検出器)。採用される範囲の最大ズレで測る。
printf '\n## Test 70j: An accepted nb_frames still leaves room for rounding (issue 397 r5)\n'
TEST_DIR="$TEST_TMP/test70j"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# 尺ベース=300 に対し nb=312 (差 12 = trust 許容ちょうど → 採用)。出力は正しいエンコード (300)
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="30/1" MOCK_NB_FRAMES=312 \
         MOCK_VIDEO_DURATION=10.0 MOCK_OUTPUT_NB_FRAMES=300 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"尺と矛盾する"* ]]; then
  printf '✓ The borderline nb_frames is actually accepted (the test exercises its claim)\n'
else
  bad '✗ 前提崩れ: nb_frames が却下されており、採用枝を検査していない\n%s\n' "$output"
fi
if [[ "$output" != *"フレーム密度不一致"* ]]; then
  printf '✓ A correct encode survives the accepted residual\n'
else
  bad '✗ A correct encode was flagged (no margin between the trust and density windows)\n%s\n' "$output"
fi

# 崖そのものの検出: trust 窓を密度窓と同値へ戻す変異を捕まえるのは **この入力だけ**。
# 🚨 上の 312/300 は余裕が 2 倍あるので、同値化の変異を緑のまま通す (6 周目が実証)。
# nb=324 は「trust が却下する / 同値化すると採用される」境界の外側で、
# 採用されると期待値 324 に対し out=299 の Δ=25 が許容 24 を超えて誤爆する。
TEST_DIR="$TEST_TMP/test70j2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="30/1" MOCK_NB_FRAMES=324 \
         MOCK_VIDEO_DURATION=10.0 MOCK_OUTPUT_NB_FRAMES=299 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム密度不一致"* ]]; then
  printf '✓ No cliff: a nb_frames just outside the trust window is rejected, not flagged\n'
else
  bad '✗ The cliff is back (trust window as wide as the density window)\n%s\n' "$output"
fi

# Test 70k: 許容の env は trust / 密度の両方で同じ検証を通る (5 周目 P2)
# 🚨 片方だけ検証すると、is_nonneg_num が通さない表記 (1e9) で 2 つの窓が別の値を使い、
# 「trust は密度の半分」が反転する (trust 無条件採用 + 密度は既定 5% = 正しいエンコードが NG)。
printf '\n## Test 70k: Tolerance env vars are validated once for both windows (issue 397 r5)\n'
TEST_DIR="$TEST_TMP/test70k"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(AV1IFY_DENSITY_TOLERANCE_PCT=1e9 MOCK_FPS="29/1" MOCK_AVG_FPS="30/1" \
         MOCK_NB_FRAMES=100000 MOCK_VIDEO_DURATION=10.0 MOCK_OUTPUT_NB_FRAMES=299 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム密度不一致"* ]]; then
  printf '✓ An unparsable AV1IFY_DENSITY_TOLERANCE_PCT does not invert the relationship\n'
else
  bad '✗ The two windows read different PCT values and flagged a correct encode\n%s\n' "$output"
fi

# 🚨 変数は 2 つある。片方だけ撃つと、もう片方の検証を外す変異が緑で通る (6 周目が実証)。
TEST_DIR="$TEST_TMP/test70k2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(AV1IFY_DENSITY_FLOOR=1e9 MOCK_FPS="29/1" MOCK_AVG_FPS="30/1" \
         MOCK_NB_FRAMES=100000 MOCK_VIDEO_DURATION=10.0 MOCK_OUTPUT_NB_FRAMES=299 \
         av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"フレーム密度不一致"* ]]; then
  printf '✓ An unparsable AV1IFY_DENSITY_FLOOR does not invert the relationship\n'
else
  bad '✗ The two windows read different FLOOR values and flagged a correct encode\n%s\n' "$output"
fi

# Test 70c: VFR でないソース (通常の CFR) では、--fps 未指定なら従来どおり検出する
# (回帰ガード: VFR 検出の追加が既存の Test 68 を無効化していないことを確認)
printf '\n## Test 70c: Frame count check still fires for genuinely CFR sources (regression guard)\n'
TEST_DIR="$TEST_TMP/test70c"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_NB_FRAMES=300 MOCK_OUTPUT_NB_FRAMES=250 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "フレーム数不一致" "CFR source still triggers frame count check"

# Test 71: 出力解像度不一致の検出
printf '\n## Test 71: Output resolution mismatch detection\n'
TEST_DIR="$TEST_TMP/test71"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# -r 720p指定で出力も720p → 正常
output=$(MOCK_OUTPUT_WIDTH=1280 MOCK_OUTPUT_HEIGHT=720 av1ify -r 720p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"解像度不一致"* ]]; then
  printf '✓ No resolution warning when output matches expected\n'
else
  bad '✗ Should not warn when resolution matches\n'
fi

# Test 73: 解像度指定なしでは解像度チェックをスキップ
printf '\n## Test 73: Resolution check skipped when no -r specified\n'
TEST_DIR="$TEST_TMP/test73"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# -r なし。出力解像度が何であっても警告しない
output=$(MOCK_OUTPUT_WIDTH=640 MOCK_OUTPUT_HEIGHT=480 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"解像度不一致"* ]]; then
  printf '✓ No resolution check when -r not specified\n'
else
  bad '✗ Should not check resolution when -r is not specified\n'
fi

# Test 74: 縦長出力の解像度チェック（短辺=widthで判定）
printf '\n## Test 74: Portrait output resolution check uses short side\n'
TEST_DIR="$TEST_TMP/test74"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
# ソース: 2160x3840(縦長4K), -r 1080p指定, 出力が1080x1920 → 短辺=1080=期待値で正常
output=$(MOCK_WIDTH=2160 MOCK_HEIGHT=3840 MOCK_OUTPUT_WIDTH=1080 MOCK_OUTPUT_HEIGHT=1920 av1ify -r 1080p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"解像度不一致"* ]]; then
  printf '✓ Portrait resolution check uses short side correctly\n'
else
  bad '✗ Portrait resolution should use short side (width)\n'
fi

# Test 75: ファイルサイズ異常の検出
printf '\n## Test 75: File size anomaly detection\n'
TEST_DIR="$TEST_TMP/test75"
mkdir -p "$TEST_DIR"
# ソースを十分大きく (100KB)
dd if=/dev/zero of="$TEST_DIR/input.avi" bs=1024 count=100 2>/dev/null
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"ファイルサイズ異常"* && "$output" != *"サイズ増加"* ]]; then
  printf '✓ No file size warning for normal ratio\n'
else
  bad '✗ Should not warn when file size ratio is normal\n'
fi

# Test 77: AV1IFY_MIN_SIZE_RATIO で閾値をカスタマイズ
printf '\n## Test 77: Custom file size ratio via AV1IFY_MIN_SIZE_RATIO\n'
TEST_DIR="$TEST_TMP/test77"
mkdir -p "$TEST_DIR"
# ソース200バイト、出力15バイト → ratio≈0.075。閾値を0.1にすると検出
dd if=/dev/zero of="$TEST_DIR/input.avi" bs=1 count=200 2>/dev/null
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"サイズ増加"* ]]; then
  printf '✓ No size increase warning when output is smaller\n'
else
  bad '✗ Should not warn when output is smaller than source\n'
fi

# Test 77d: サイズ増加時に増加率(%)が表示される
printf '\n## Test 77d: Size increase percentage is shown\n'
TEST_DIR="$TEST_TMP/test77d"
mkdir -p "$TEST_DIR"
# ソース10B、モック出力16B → +60%
dd if=/dev/zero of="$TEST_DIR/input.avi" bs=1 count=10 2>/dev/null
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_FFMPEG_OUTPUT_SIZE=16 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "+60%" "Shows correct percentage increase"

# Test 78: 出力映像コーデック不一致の検出
printf '\n## Test 78: Output video codec mismatch detection\n'
TEST_DIR="$TEST_TMP/test78"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_OUTPUT_VCODEC=av1 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
if [[ "$output" != *"映像コーデック不一致"* ]]; then
  printf '✓ No codec warning when output is av1\n'
else
  bad '✗ Should not warn when output codec is av1\n'
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
    bad '✗ "%s" should be accepted (rc=%d, err=%q)\n' "$v" "$rc" "$err"
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
    bad '✗ "%s" should be accepted (rc=%d, err=%q)\n' "$v" "$rc" "$err"
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
    bad '✗ "%s" should be rejected silently (rc=%d, err=%q)\n' "$v" "$rc" "$err"
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
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
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
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_OUTPUT_AUDIO_INDEX= __av1ify_postcheck "$TEST_DIR/video-enc.mp4" "$TEST_DIR/ghost.avi" 0 "" 2>&1)
rc=$?
setopt err_exit
assert_contains "$output" "音声ストリーム検出できず" "Reports noaudio when source is missing"
(( rc != 0 )) && printf '✓ postcheck returns non-zero (rc=%d)\n' "$rc" || { bad '✗ postcheck should return non-zero\n'; exit 1; }
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
cd "$TEST_DIR" || exit 1
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
(( rc != 0 )) && printf '✓ postcheck returns non-zero (rc=%d)\n' "$rc" || { bad '✗ postcheck should return non-zero\n'; exit 1; }

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
  bad '✗ Expected input-check_ng2-enc.mp4, got: %s\n' "$marked"; exit 1
fi
if [[ "$(cat "$TEST_DIR/input-check_ng-enc.mp4")" == "previous artifact" ]]; then
  printf '✓ Previous artifact preserved\n'
else
  bad '✗ Previous artifact was overwritten\n'; exit 1
fi
(( rc == 0 )) || { bad '✗ mark_issue should return 0\n'; exit 1; }

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
(( rc == 1 )) || { bad '✗ finalize should return 1 (NG) on missing output\n'; exit 1; }

printf '\n=== Postcheck Tests Completed ===\n'
