#!/usr/bin/env bash
# check_assert_reaches_exit.sh — 「✗ を出しても exit code に出ない」テストを実験で洗い出す。
#
# なぜ (issue 327 が正本。先行事例は issue 139):
#   runner (Makefile の run_tests) は **exit code しか見ない**。出力の `✗` は一切見ていないので、
#   「✗ を printf するだけで非 0 にならない」アサーションは、**失敗しても [ok] に集計される**。
#   実測 2026-09-07: tests/zshrc/av1ify/test_av1ify_prefetch.sh に issue 306 の看取りを
#   無効化する変異を当てると `✗` は出たが rc=0 のままだった。
#
# 🚨 静的検査では母集合を確定できない (issue 327 で実際に外した)。
#   「✗ を含むが exit 1 が無いファイル」で grep すると、`cd "$X" || exit 1` が正規表現に当たって
#   当の test_av1ify_prefetch.sh が漏れる。判定できるのは
#   **「アサーションを 1 つ壊したときに rc が非 0 になるか」を実際に走らせて見る**ことだけ。
#
# やること: テストファイルごとに
#   1. `✗` を報告する分岐を見つけ、**その分岐へ必ず入るように条件を固定する**
#      (`if COND; then ✓ else ✗ fi` → `if false; then` / ✗ が then 側なら `if true; then`。
#       `COND || bad '✗…'` なら左辺を false にする)。報告の形 (printf / bad / fail / assert_*) は
#      ファイル自身のものをそのまま通るので、引数の数や書式を推測しなくてよい
#   2. コピーを作ってその 1 行だけを差し替え、走らせて rc を見る
#   3. 位置による違いを拾うため **最初と最後の候補**の 2 箇所を別々に試す
#      (fail カウンタを末尾でしか見ないファイル / 途中だけ exit するファイルを分けるため)
#
# 🚨 結果は 3 値。「判定不能」を合格にも不合格にも丸めない
#    (_claude/rules/adversarial-review-own-safeguards.md 節 2):
#     OK    : 試した箇所すべてで rc が非 0 になった
#     出ない: いずれかの箇所で rc=0 のままだった (= その失敗は CI から不可視)
#     不明  : 条件を固定できる分岐が無い / コピーの素の実行が既に非 0
#             (skip・環境依存・自分のファイル名への依存) / timeout
#
# 検出しないと決めたもの (脅威モデル。_claude/rules/adversarial-review-own-safeguards.md 節 8):
#   - **試した 2 箇所以外の assert**。同じファイルが場所によって別の書き方をしていると
#     取りこぼす。ここは人のレビューの責務
#   - **サブシェル / パイプ右辺でだけ失敗する形** (親へ `fail=1` が返らない形)。
#     条件を固定してもその分岐が親の rc に効かないので「出ない」と出るが、
#     逆に「サブシェル内なのに rc に出た」ように見える偽の OK は作らない
#   - **条件の固定が別の理由で失敗を招く場合** (固定した結果あとで unset 変数に触る等)。
#     rc が非 0 になるので OK 側に倒れる = 見落とす向き
#   - 複数行にまたがる条件 (`if` 行が `; then` で終わらない形) は判定不能にする
#   - 🚨 **失敗のマーカーが `✗` 以外のファイル**。この道具は `✗` の行しか探さないので、
#     `FAIL:` / `ERROR` で報告するファイル (実測 2026-09-08 で 16 件) は
#     「✗ を出さない」に落ちて**一度も検査されない**。マーカーを増やすと `MOCK_FFMPEG_FAIL` の
#     ような変数名にも当たるため、ここは広げずに issue 329 の残債として別に確かめる
#   - **末尾の候補がアサーションでなく集計行のことがある** (`if (( FAIL_COUNT > 0 )); then ✗ 失敗 N 件`)。
#     2 サンプルの片方をそこに使ってしまうが、偽の OK は作らない (どれか 1 つでも rc=0 なら「出ない」)
#
# 使い方:
#   scripts/check_assert_reaches_exit.sh              # tests/ 全体 (時間がかかる)
#   scripts/check_assert_reaches_exit.sh tests/zshrc  # 対象を絞る
#   PROBE_JOBS=1 ...   # 共有資源に触るテストで結果が揺れるときは直列にする
#   PROBE_TIMEOUT=300 ...
#
# 🚨 **当面は赤いのが正常**。issue 327 の時点で tests/ の多くが「✗ を printf するだけ」で書かれており、
#   是正は 1 ファイルずつ進める (残りの母集合は issue 327 とその follow-up に記録してある)。
#   「赤 = 自分が壊した」ではないので、出力の一覧を issue の母集合と突き合わせること。
#
# 🚨 make test には入れない (各テストを 3 回走らせるので所要時間が数倍になる)。
#   棚卸しとして手で叩く (入口は tests/CLAUDE.md「アサーションの失敗は exit code に出す」)。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || exit 1

JOBS="${PROBE_JOBS:-4}"
export PROBE_TIMEOUT="${PROBE_TIMEOUT:-180}"

targets=("${@:-tests}")
mapfile -t FILES < <(find "${targets[@]}" -type f -name 'test_*.sh' ! -name '*helper*' -print | sort)
if [ "${#FILES[@]}" -eq 0 ]; then
  echo "✗ 検査対象のテストが 1 件も見つからない (find のパスが壊れている)" >&2
  exit 1
fi

WORK="$(mktemp -d)"; export WORK
# 🚨 プローブのコピーは**テストと同じディレクトリ**に置く (多くのテストが ${0:A:h} で helper を
# source するため、別ディレクトリへコピーすると helper ごと見失う)。後始末は 1 本の EXIT trap に
# 束ねる (trap は 1 つしか持てない)。
cleanup() {
  find "${targets[@]}" -type f -name '.probe-*' -delete 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

# 「✗ へ必ず入るように条件を固定する」差し替え候補を列挙する。
# 出力: "<✗ の行>\t<書き換える行>\t<書き換え後の内容>"
probe_plan() {
  awk '
    { L[NR] = $0 }
    END {
      for (k = 1; k <= NR; k++) {
        line = L[k]
        if (index(line, "✗") == 0) continue
        s = line; sub(/^[[:space:]]*/, "", s)
        if (s ~ /^#/) continue

        # 形 B: `COND || bad "✗…"` — 左辺を false にすれば必ず報告へ入る
        if (match(line, /\|\|/) && index(substr(line, 1, RSTART), "✗") == 0) {
          ind = line; sub(/[^[:space:]].*$/, "", ind)
          printf "%d\t%d\t%sfalse %s\n", k, k, ind, substr(line, RSTART)
          continue
        }

        # 形 A: if/elif の分岐の中。else 側なら false、then 側なら true で固定する
        saw_else = 0; else_ind = ""
        for (j = k - 1; j >= 1 && j >= k - 40; j--) {
          t = L[j]; u = t; sub(/^[[:space:]]*/, "", u)
          if (u == "else" || u ~ /^else[[:space:]]/) {
            if (!saw_else) { saw_else = 1; else_ind = t; sub(/[^[:space:]].*$/, "", else_ind) }
            continue
          }
          if (u ~ /^(if|elif)[[:space:]].*;[[:space:]]*then[[:space:]]*$/) {
            ind = t; sub(/[^[:space:]].*$/, "", ind)
            kw = (u ~ /^elif/) ? "elif" : "if"
            forced = (saw_else && else_ind == ind) ? "false" : "true"
            printf "%d\t%d\t%s%s %s; then\n", k, j, ind, kw, forced
            break
          }
          # 別の分岐の閉じに当たったらこの ✗ の外側なので諦める
          if (u == "fi") break
        }
      }
    }
  ' "$1"
}

# コピーを 1 回走らせて rc を返す (timeout つき)。
run_probe() {
  local probe="$1" rc pid killer
  chmod +x "$probe"
  "$probe" >/dev/null 2>&1 &
  pid=$!
  ( sleep "$PROBE_TIMEOUT"; kill -9 "$pid" 2>/dev/null ) & killer=$!
  wait "$pid"; rc=$?
  kill "$killer" 2>/dev/null; wait "$killer" 2>/dev/null
  return "$rc"
}

probe_one() {
  local f="$1"
  local dir base probe plan first last rc tag
  dir="$(dirname "$f")"; base="$(basename "$f")"
  # 🚨 一時名を $$ だけで作らない。timeout で殺した worker が残骸を置いたまま pid が再利用されると、
  # 別のファイルのプローブを掴んで **もっともらしい「出ない」/「不明」** を作る (エラーにならないので
  # 出力の表からは見分けられない)。対象のパスから作った tag を混ぜて衝突しない名前にする。
  tag="$(printf '%s' "$f" | cksum | tr -d ' ')-$$-$RANDOM"
  probe="$dir/.probe-$tag-$base"
  plan="$WORK/plan.$tag"

  probe_plan "$f" > "$plan" 2>/dev/null
  if [ ! -s "$plan" ]; then
    if grep -q '✗' "$f"; then
      printf '不明\t%s\t✗ へ入る条件を固定できる分岐が無い (書き方が想定外)\n' "$f"
    else
      printf '対象外\t%s\t✗ を出す行が無い\n' "$f"
    fi
    rm -f "$plan"; return 0
  fi

  # baseline: **コピーを素のまま**走らせる (自分のファイル名への依存・skip・既存の赤をここで弾く)
  cp "$f" "$probe"
  if ! run_probe "$probe"; then
    rc=$?
    rm -f "$probe" "$plan"
    printf '不明\t%s\tコピーの素の実行が rc=%s (skip / 環境依存 / 自分のファイル名への依存 / timeout)\n' "$f" "$rc"
    return 0
  fi

  first="$(head -1 "$plan")"; last="$(tail -1 "$plan")"
  local verdict='OK' detail='' tried=0 entry
  for entry in "$first" "$last"; do
    [ -n "$entry" ] || continue
    [ "$tried" -eq 1 ] && [ "$entry" = "$first" ] && continue
    tried=$((tried + 1))
    local xline mline mtext
    xline="$(cut -f1 <<< "$entry")"; mline="$(cut -f2 <<< "$entry")"; mtext="$(cut -f3- <<< "$entry")"
    awk -v ln="$mline" -v txt="$mtext" 'NR == ln { print txt; next } { print }' "$f" > "$probe"
    if run_probe "$probe"; then
      verdict='出ない'
      detail="${detail:+$detail / }${base}:${xline} の失敗が rc に出ない"
    fi
  done
  rm -f "$probe" "$plan"
  printf '%s\t%s\t%s\n' "$verdict" "$f" "${detail:-試した ${tried} 箇所すべてで rc が非 0}"
}

export -f probe_one probe_plan run_probe

RESULTS="$WORK/results"
printf '%s\n' "${FILES[@]}" | xargs -P "$JOBS" -I{} bash -c 'probe_one "$@"' _ {} > "$RESULTS"
sort -o "$RESULTS" "$RESULTS"

ok=$(grep -c '^OK	' "$RESULTS" || true)
bad=$(grep -c '^出ない	' "$RESULTS" || true)
unk=$(grep -c '^不明	' "$RESULTS" || true)
na=$(grep -c '^対象外	' "$RESULTS" || true)
reported=$((ok + bad + unk + na))

if [ "$bad" -gt 0 ]; then
  echo ""
  echo "✗ アサーションの失敗が exit code に出ないテスト ($bad 件):"
  awk -F'\t' '$1 == "出ない" { printf "  %s\n    %s\n", $2, $3 }' "$RESULTS"
fi
if [ "$unk" -gt 0 ]; then
  echo ""
  echo "? 判定不能 ($unk 件。合格でも不合格でもない):"
  awk -F'\t' '$1 == "不明" { printf "  %s\n    %s\n", $2, $3 }' "$RESULTS"
fi

echo ""
echo "検査 ${#FILES[@]} 件 / 結果を報告 $reported 件: rc に出る $ok / 出ない $bad / 判定不能 $unk / ✗ を出さない $na"
# 🚨 「対象 N 件」と「報告 N 件」が合わないのは runner 自体の故障 (run_tests_parallel と同じ規律)
if [ "$reported" -ne "${#FILES[@]}" ]; then
  echo "✗ 結果を報告しなかったファイルがある (対象 ${#FILES[@]} / 報告 $reported)" >&2
  exit 1
fi
[ "$bad" -eq 0 ] && [ "$unk" -eq 0 ]
