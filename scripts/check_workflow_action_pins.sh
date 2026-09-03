#!/usr/bin/env bash
# check_workflow_action_pins.sh — 同じ GitHub Action が workflow 間で違う版に固定されるのを落とす。
#
# なぜ (issue 073 §1 / issue 141):
#   073 は「shellcheck は版を固定したのに actionlint だけ未固定だった」= **同じ教訓の横展開漏れ**。
#   141 は brew 導入ブロックが 3 workflow に逐語重複していたのを composite へ集約した。
#   どちらも「同じことを書く場所が複数あり、片方だけ古くなる」形。
#
#   実害の出方が地味なのが厄介: 版が割れていても workflow は**動く**ので、actionlint も
#   `make test-actionlint` も緑のまま。気づくのは「片方の lane でだけ再現するビルド差」を
#   追い始めたとき。実測 2026-09-03: doctor.yml だけ actions/setup-go@v6、他 3 箇所は @v7 だった。
#   doctor.yml は「doctor を触ったとき最初に赤くなる先行指標」として作った横断レーンなので、
#   **先行指標だけ別の Go セットアップで走る**状態になっていた。
#
# 検出する形: `uses: <owner>/<action>@<ref>` を全 workflow / composite から集め、
#   同じ action が 2 種類以上の ref を持っていたら落とす。
#
# 意図的に版を分ける (移行の途中など) 場合は、その行に `action-pin: allow <理由>` を書く。
#
# 🚨 0 件でも「uses: が 1 つも見つからない」なら失敗にする (パスや正規表現が壊れたら赤にする)。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || exit 1

WF_DIR=".github"

# action 名 -> ref の一覧 (allow マーカーの行は除く)
pairs=$(
  grep -rhnE '^[[:space:]]*(-[[:space:]]+)?uses:[[:space:]]*[^[:space:]]+@[^[:space:]]+' "$WF_DIR" 2>/dev/null |
    grep -v 'action-pin: allow' |
    sed -E 's/.*uses:[[:space:]]*//; s/[[:space:]]*(#.*)?$//' |
    sort -u
)

if [ -z "$pairs" ]; then
  echo "✗ uses: の行が 1 つも見つからない ($WF_DIR のパスか正規表現が壊れている)" >&2
  exit 1
fi

total=$(wc -l <<< "$pairs" | tr -d ' ')
violations=0
while IFS= read -r name; do
  [ -n "$name" ] || continue
  refs=$(grep -E "^${name}@" <<< "$pairs" | sed -E 's/^[^@]+@//' | sort -u)
  count=$(wc -l <<< "$refs" | tr -d ' ')
  [ "$count" -le 1 ] && continue
  printf '✗ %s が %s 種類の版で使われている: %s\n' "$name" "$count" "$(tr '\n' ' ' <<< "$refs")"
  grep -rnE "uses:[[:space:]]*${name}@" "$WF_DIR" | sed 's/^/    /'
  violations=$((violations + 1))
done <<< "$(sed -E 's/@.*//' <<< "$pairs" | sort -u)"

if [ "$violations" -gt 0 ]; then
  echo "✗ 版が割れている action: $violations 件 (issue 073 §1)" >&2
  echo "  意図的なら行内に 'action-pin: allow <理由>' を書く" >&2
  exit 1
fi

echo "OK: workflow の action 版を検査した ($total 種類)"
