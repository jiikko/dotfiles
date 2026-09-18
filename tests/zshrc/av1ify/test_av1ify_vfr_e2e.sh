#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify VFR 正規化の e2e テスト (issue 394)。mock を使わず本物の ffmpeg / ffprobe で走らせる。
#
# 守る不変条件:
#   1. VFR ソース (r_frame_rate と avg_frame_rate が乖離) の出力は CFR になる
#      (r_frame_rate == avg_frame_rate、映像パケット長が 1 種類)
#   2. VFR と判定されないソースは改修前と同じ引数 (フレーム数が変わらず、メッセージも出ない)
# 🚨 「DTS 非単調増加」は判定に使わない。ffmpeg -f null の警告は検証側の fps 変換が出すもので、
#    ファイルの欠陥ではなかった (issue 394 の訂正節)。ここではパケットを ffprobe で直接読む。
#
# fixture は 6〜10 秒 / 160x120。音声を付けるのは、無いと av1ify の最終検証が no-audio で NG にするため。

setopt err_exit no_unset pipe_fail

ROOT_DIR="${0:A:h}/../../.."
ROOT_DIR="${ROOT_DIR:A}"

# 実ツールが無い環境では丸ごと skip (exit 77 = runner が [skip] と数える。0 にすると [ok] に化ける)
if ! command -v ffmpeg >/dev/null 2>&1 || ! command -v ffprobe >/dev/null 2>&1; then
  print -r -- "SKIP: ffmpeg / ffprobe が無い"; exit 77
fi
if ! ffmpeg -hide_banner -h encoder=libsvtav1 >/dev/null 2>&1; then
  print -r -- "SKIP: ffmpeg に libsvtav1 が無い"; exit 77
fi

TEST_TMP="$(mktemp -d)"
export AV1IFY_LOCK_ROOT="$TEST_TMP/lockman"
# 排他 (lockman) はこのテストの対象外。CI の heavy には go が無く lockman をビルドできないので、
# 無いときだけ排他なしで続行させる (lockman がある環境では従来どおり排他を取る)。
export AV1IFY_ALLOW_NO_LOCK=1
typeset -gi FAIL_COUNT=0
bad() { printf "$@"; FAIL_COUNT=$(( FAIL_COUNT + 1 )); }
cleanup() {
  local rc=$?
  rm -rf "$TEST_TMP"
  if (( FAIL_COUNT > 0 )); then
    printf '✗ 失敗 %d 件\n' "$FAIL_COUNT" >&2
    exit 1
  fi
  return $rc
}
trap cleanup EXIT

# 速度優先 (テストで見たいのはフレームタイミングであって画質ではない)
export AV1_PRESET=12

source "$ROOT_DIR/zshlib/_av1ify.zsh"

check_eq() {  # <actual> <expected> <message>
  if [[ "$1" == "$2" ]]; then printf '✓ %s\n' "$3"; else bad '✗ %s (got=%s want=%s)\n' "$3" "$1" "$2"; fi
}
v_field() { ffprobe -v error -select_streams v:0 -show_entries "stream=$2" -of default=nk=1:nw=1 -- "$1" | head -n1; }
pkt_duration_kinds() { ffprobe -v error -select_streams v:0 -show_entries packet=duration -of csv=p=0 -- "$1" | sort -u | wc -l | tr -d ' '; }

printf '\n=== av1ify VFR e2e (real ffmpeg) ===\n\n'

# --- Test 1: VFR ソース → CFR 出力 ---
printf '## Test 1: VFR source is encoded to CFR\n'
d="$TEST_TMP/vfr"; mkdir -p "$d"
# 30fps から 7 フレームに 1 枚を間引き、元のタイムスタンプのまま残す (= VFR)
ffmpeg -nostdin -v error -y \
  -f lavfi -i "testsrc=size=160x120:rate=30:duration=6" -f lavfi -i "sine=frequency=440:duration=6" \
  -vf "select='not(eq(mod(n\,7)\,3))'" -fps_mode passthrough -c:v mpeg4 -c:a aac -shortest "$d/in.mp4"
# 前提: fixture が本番の判定で VFR になっていること (崩れたら以降の assert は何も守らないので打ち切る)
__av1ify_detect_vfr "$d/in.mp4"
if (( ! __AV1IFY_R_VFR_DETECTED )); then
  bad '✗ 前提崩れ: fixture が VFR と判定されない (r=%s avg=%s)\n' "$(v_field "$d/in.mp4" r_frame_rate)" "$(v_field "$d/in.mp4" avg_frame_rate)"
  exit 1
fi
if (( $(pkt_duration_kinds "$d/in.mp4") < 2 )); then
  bad '✗ 前提崩れ: fixture のパケット長が 1 種類 (VFR になっていない)\n'; exit 1
fi
out_log="$(av1ify "$d/in.mp4" 2>&1)" || bad '✗ av1ify が失敗した\n%s\n' "$out_log"
out="$d/in-enc.mp4"
if [[ ! -f "$out" ]]; then
  bad '✗ 出力 %s が無い (check_ng に転んだ可能性)\n%s\n' "${out:t}" "$(ls "$d")"
else
  check_eq "$(v_field "$out" codec_name)" "av1" "出力が AV1 でエンコードされている"
  [[ "$out_log" == *"可変フレームレート (VFR)"* ]] && printf '✓ VFR 正規化のメッセージが出る\n' || bad '✗ VFR 正規化のメッセージが出ない\n'
  check_eq "$(v_field "$out" avg_frame_rate)" "$(v_field "$out" r_frame_rate)" "出力の avg_frame_rate == r_frame_rate (CFR)"
  check_eq "$(pkt_duration_kinds "$out")" "1" "出力の映像パケット長が 1 種類 (CFR)"
  # 🚨 「CFR である」だけを見ないこと (issue 397)。どんな -r でも出力は CFR になるので、
  # 上の 2 つは **何 fps の CFR か** に対して無感覚で、-r 10 のような誤りを緑のまま通す
  # (実測: ソース 154 フレーム中 92 フレームが drop されても 7/7 緑だった)。
  # ソースの avg_frame_rate と突き合わせて **値そのもの** を pin する。
  check_eq "$(v_field "$out" r_frame_rate)" "$(v_field "$d/in.mp4" avg_frame_rate)" "出力の r_frame_rate == ソースの avg_frame_rate (値の pin)"
  # 密度: 出力フレーム数がソースの尺 × 適用 fps と一致する (postcheck の密度検査と同じ主張を実物で)
  src_dur="$(ffprobe -v error -show_entries format=duration -of csv=p=0 -- "$d/in.mp4")"
  exp_frames="$(LC_ALL=C awk -v dur="$src_dur" -v fps="$(v_field "$d/in.mp4" avg_frame_rate)" 'BEGIN {
    split(fps, a, "/"); printf "%d", int(dur * a[1] / a[2] + 0.5) }')"
  out_frames="$(v_field "$out" nb_frames)"
  if (( out_frames >= exp_frames - 2 && out_frames <= exp_frames + 2 )); then
    printf '✓ 出力フレーム数が期待密度と一致 (期待≈%s, out=%s)\n' "$exp_frames" "$out_frames"
  else
    bad '✗ 出力フレーム数が期待密度と乖離 (期待≈%s, out=%s)\n' "$exp_frames" "$out_frames"
  fi
fi

# --- Test 2: VFR と判定されないソース → 改修前と同じ引数 (フレーム数が変わらない) ---
# fixture は「間隔に乱れはあるが VFR 判定には掛からない」形 (10 秒から 1 フレーム抜く。
# avg との差 0.33% < 0.5%)。実ファイル 91 本中 36 本がこの形で、cfr を付けるとフレーム数が
# 変わる (ここでは 299 → 300)。綺麗な CFR fixture だと cfr は no-op なので、
# 「cfr を常時付与する」退行を検出できない (変異で確認済み)。
printf '\n## Test 2: A non-VFR source (with a small gap) keeps its frame count\n'
d="$TEST_TMP/cfr"; mkdir -p "$d"
ffmpeg -nostdin -v error -y \
  -f lavfi -i "testsrc=size=160x120:rate=30:duration=10" -f lavfi -i "sine=frequency=440:duration=10" \
  -vf "select='not(eq(n\,150))'" -fps_mode passthrough -c:v mpeg4 -c:a aac -shortest "$d/in.mp4"
__av1ify_detect_vfr "$d/in.mp4"
(( __AV1IFY_R_VFR_DETECTED )) && { bad '✗ 前提崩れ: fixture が VFR と判定された\n'; exit 1; }
if (( $(pkt_duration_kinds "$d/in.mp4") < 2 )); then
  bad '✗ 前提崩れ: fixture に間隔の乱れが無い (cfr の常時付与を検出できない)\n'; exit 1
fi
src_frames="$(v_field "$d/in.mp4" nb_frames)"
out_log="$(av1ify "$d/in.mp4" 2>&1)" || bad '✗ av1ify が失敗した\n%s\n' "$out_log"
out="$d/in-enc.mp4"
if [[ ! -f "$out" ]]; then
  bad '✗ 出力 %s が無い\n%s\n' "${out:t}" "$(ls "$d")"
else
  check_eq "$(v_field "$out" codec_name)" "av1" "出力が AV1 でエンコードされている"
  [[ "$out_log" != *"可変フレームレート (VFR)"* ]] && printf '✓ VFR 正規化のメッセージが出ない\n' || bad '✗ CFR ソースで VFR 正規化のメッセージが出た\n'
  check_eq "$(v_field "$out" nb_frames)" "$src_frames" "出力のフレーム数がソースと同じ"
fi

printf '\n=== av1ify VFR e2e Completed ===\n\n'
