#!/usr/bin/env bash
# bench スクリプトの "metric=<name> ms=<value>" 出力を予算ファイルと突き合わせ、
# 超過があれば非 0 で終了する (CI のデグレ検出用)。
#
# 使い方: tests/nvim/bench_nvim.sh | tests/check_bench_budgets.sh tests/nvim/bench_budgets.ci
#
# 予算ファイルの形式 ('#' 始まりと空行は無視):
#   <metric名> <上限ms>            … 絶対予算
#   <metric名> <上限ms> rel        … 較正スケール対象 (calibrate 宣言が必須)
#   calibrate <metric名> <基準ms>  … 較正器の宣言 (rel の上限を max(1, 実測/基準) 倍する)
# 各予算値の決め方 (粗い安全網か「現在速度」ゲートか) は予算ファイル側のコメントが一次情報。
#
# rel / calibrate は shared runner の混雑ノイズ対策 (2026-07-17 導入): 混雑はループ系
# metric に乗法的に乗る (実測: run 29547499619 の attempt1/2 比較で rtt が ×3.32 のとき
# new_window ×3.26 / kill_window ×3.47 / select ×3.33 と一様)。較正器 = repo コードの
# 影響が小さい round-trip 系 metric の膨張率で rel 上限をスケールすると、混雑 run では
# 誤爆せず、静穏 run では従来の厳しさのまま。真の回帰は「較正器が静かなのに rel metric
# だけ跳ねる」形になるため検出能力は落ちない。較正器自身は絶対予算で別途ゲートして
# ドリフトを検知する (rel には出来ない。checker が構成エラーとして拒否する)。
# ⚠️ 「一様に乗る」が成り立つのは較正器と同じコスト系の metric だけ: fork/プロセス生成が
# 主コストの metric (tmux の window/pane churn) は、round-trip 系の較正器が min 集約で
# 静穏に見える run でも 1.3〜1.4 倍膨らむ (実測 run 30790932059)。そちら側の予算は
# rel のスケールに頼らず実測中央値に対する余裕で持たせる (予算ファイル側のコメント参照)。
# スケールが CALIB_MAX_SCALE を超える極端な混雑では、その倍率の数字は計測として
# 無意味なため rel の gating を警告のみに落とす (絶対予算 metric は引き続き enforce)。
#
# 検出するもの:
#   - metric の予算超過 (rel は較正スケール後の上限で判定)
#   - metric の ms が数値でない (bench 側の計測失敗の素通り防止)
#   - 予算ファイルに載っている metric が出力に無い (bench 自体の失敗・metric 改名漏れ)
#   - 予算ファイルの上限が数値でない / calibrate 構成の誤り (予算ファイル破損)
#
# 注: 同名 metric が複数回来た場合は最後の値で判定する (bench_stats.sh 経由の
# 正規運用では metric は 1 行ずつしか来ない)。
set -uo pipefail

budget_file="${1:?usage: bench.sh | check_bench_budgets.sh <budget-file>}"

CALIB_MAX_SCALE=4

# 表示単位は metric 名の接尾辞が持つ (051 の判断: 予算ファイルの行書式に単位トークンを
# 増やさない。行の項目名は常に ms= のまま、意味の単位だけ名前から引く)。
# server_rss_mb は MB、*_alloc_kb は KB で、超過文言を常に ms と書くと時間の回帰に見え、
# 対処 (確保 = 絶対値 +3% / 時間 = 較正つき rel) の入口を誤らせる (issue 064)
unit_of() {
  case "$1" in
    *_kb) printf 'KB' ;;
    *_mb) printf 'MB' ;;
    *) printf 'ms' ;;
  esac
}

declare -A budget rel seen value
calib_metric="" calib_ref=""
while read -r name limit extra _; do
  [[ -z "$name" || "$name" == \#* ]] && continue
  if [[ "$name" == calibrate ]]; then
    calib_metric="$limit"; calib_ref="${extra:-}"
    if [[ -z "$calib_metric" ]] || ! [[ "$calib_ref" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
      printf '::error::budget malformed: "calibrate %s %s" (%s)\n' "$calib_metric" "$calib_ref" "$budget_file"
      exit 1
    fi
    continue
  fi
  # 上限が数値でない (予算ファイル破損) は照合を始める前に loud に落とす
  if ! [[ "$limit" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
    printf '::error::budget malformed: "%s %s" (%s)\n' "$name" "$limit" "$budget_file"
    exit 1
  fi
  budget[$name]="$limit"
  [[ "${extra:-}" == rel ]] && rel[$name]=1
done < "$budget_file"

# 較正器を rel にすると自分で自分をスケールする自己参照になり、混雑でも回帰でも
# 常に pass する構成事故になるため拒否する
if [[ -n "$calib_metric" && -n "${rel[$calib_metric]:-}" ]]; then
  printf '::error::budget malformed: 較正器 %s は rel にできない (%s)\n' "$calib_metric" "$budget_file"
  exit 1
fi
# rel があるのに calibrate 宣言が無いのも構成事故 (スケール 1 で黙って動くと意図が消える)
if [[ -z "$calib_metric" ]]; then
  for name in "${!rel[@]}"; do
    printf '::error::budget malformed: %s が rel だが calibrate 宣言が無い (%s)\n' "$name" "$budget_file"
    exit 1
  done
fi

fail=0
while IFS= read -r line; do
  printf '%s\n' "$line"
  [[ "$line" == metric=* ]] || continue
  name="${line#metric=}"; name="${name%% *}"
  ms="${line##*ms=}"
  seen[$name]=1
  # ms が数値でない (bench 側の計測失敗の素通り) を fail にする。awk の v+0 は
  # 空文字や文字列を 0 に潰すため、ここで弾かないと予算照合が黙って pass する
  if ! [[ "$ms" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
    printf '::error::bench metric malformed: %s ms="%s" (数値でない。bench 側の計測失敗)\n' "$name" "$ms"
    fail=1
    continue
  fi
  value[$name]="$ms"
done

# 較正スケールの決定。rel 判定は metric の出現順に依存しないよう、全行を読み終えてから行う
scale=1
skip_rel=0
if [[ -n "$calib_metric" ]]; then
  if [[ -z "${value[$calib_metric]:-}" ]]; then
    printf '::error::bench metric missing: %s (較正器。bench 失敗か metric 改名漏れ)\n' "$calib_metric"
    exit 1
  fi
  scale=$(awk -v v="${value[$calib_metric]}" -v r="$calib_ref" 'BEGIN { s = v / r; if (s < 1) s = 1; printf "%.2f", s }')
  if awk -v s="$scale" 'BEGIN { exit !(s > 1) }'; then
    printf 'bench calibration: %s=%sms / ref %sms -> rel 上限を %s 倍 (runner 混雑補正)\n' \
      "$calib_metric" "${value[$calib_metric]}" "$calib_ref" "$scale"
  fi
  if awk -v s="$scale" -v m="$CALIB_MAX_SCALE" 'BEGIN { exit !(s > m) }'; then
    printf '::warning::runner 混雑が極端 (較正 %s 倍 > %s 倍)。rel metric の gating はこの run では警告のみ\n' "$scale" "$CALIB_MAX_SCALE"
    skip_rel=1
  fi
fi

for name in "${!value[@]}"; do
  limit="${budget[$name]:-}"
  [[ -z "$limit" ]] && continue
  eff="$limit"
  if [[ -n "${rel[$name]:-}" ]]; then
    eff=$(awk -v l="$limit" -v s="$scale" 'BEGIN { printf "%.1f", l * s }')
  fi
  if awk -v v="${value[$name]}" -v l="$eff" 'BEGIN { exit !(v + 0 > l + 0) }'; then
    if [[ -n "${rel[$name]:-}" && "$skip_rel" == 1 ]]; then
      printf '::warning::bench over budget (極端な混雑 run のため警告のみ): %s %s%s > %s%s (%s)\n' "$name" "${value[$name]}" "$(unit_of "$name")" "$eff" "$(unit_of "$name")" "$budget_file"
    else
      printf '::error::bench regression: %s %s%s > budget %s%s (%s)\n' "$name" "${value[$name]}" "$(unit_of "$name")" "$eff" "$(unit_of "$name")" "$budget_file"
      fail=1
    fi
  fi
done

for name in "${!budget[@]}"; do
  if [[ -z "${seen[$name]:-}" ]]; then
    printf '::error::bench metric missing: %s (bench 失敗か metric 改名漏れ。%s も更新すること)\n' "$name" "$budget_file"
    fail=1
  fi
done

exit "$fail"
