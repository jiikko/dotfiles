#!/usr/bin/env bash
# schedule で起こした run が本体の job を走らせるかを決め、GITHUB_OUTPUT へ足す行を出す:
#   run=true                       本体を走らせる
#   run=false + prior=success      この commit は検証済みで緑。gate だけで緑で終える
#   run=false + prior=failure      この commit は検証済みで赤。呼び出し側の step が gate だけの run を取り消す
#
#   scripts/ci_schedule_gate.sh <workflow のファイル名> <commit の sha> <owner/repo> <この run の id>
#
# push が数分おきに続くと、Tests / Bench の run は cancel-in-progress で次の push に取り消され続け、完走する run が途切れる。
# schedule の run はそれを埋めるためのもので、<sha> (master の最新) に「本体の job が実際に走って結果を出した run」があれば、
# 同じ commit を重ねて検証せず、その結果を引き継ぐ。
# 🚨 run の結論だけで数えない: gate だけで終わった run (本体は skip) も success / failure になる。数えると、テストを 1 度も
#    走らせていない commit を以後も走らせない。本体が走ったかは job の一覧 (schedule-gate 以外の job の結論) で見る。
# 🚨 走っている途中の run を「これから結果が出る」と数えない: push が続く場面では、その run は次の push で取り消される
#    (数えると、この仕組みが要る場面でだけ毎回見送る)。
# 🚨 前の結果が赤なら、gate だけの run を緑で終えない: 新しい順で run を選ぶ読み手 (bin/ci-log -i) に赤を緑で隠す。
#    赤で終えるのも採らない (push が無い間に赤い run が毎時積み重なり、push の run を re-run で緑にしても赤が残る)。取り消して終える。
# 問い合わせに失敗したら run=true にする (検証を飛ばすより、1 本余分に走らせる方が安い)。理由は stderr に出す。
set -euo pipefail

[ $# -eq 4 ] || { echo "usage: scripts/ci_schedule_gate.sh <workflow のファイル名> <sha> <owner/repo> <この run の id>" >&2; exit 2; }
wf="$1" sha="$2" repo="$3" self="$4"
case "$self" in ''|*[!0-9]*) echo "ci_schedule_gate: run の id が数字でない: $self" >&2; exit 2 ;; esac

fail_open() { echo "ci_schedule_gate: $1。本体を走らせる" >&2; echo run=true; exit 0; }

# この commit の、終わった (success / failure) 自分以外の run を新しい順に
ids="$(gh api "repos/$repo/actions/workflows/$wf/runs?head_sha=$sha&status=completed&per_page=100" \
        --jq "[.workflow_runs[] | select(.id != $self) | select(.conclusion == \"success\" or .conclusion == \"failure\")]
              | sort_by(.created_at) | reverse | .[] | \"\(.id) \(.conclusion)\"")" ||
  fail_open "$wf の run を問い合わせられない"

while read -r id concl; do
  [ -n "$id" ] || continue
  n="$(gh api "repos/$repo/actions/runs/$id/jobs?per_page=100" \
        --jq '[.jobs[] | select(.name != "schedule-gate") | select(.conclusion == "success" or .conclusion == "failure")] | length')" ||
    fail_open "run $id の job を問い合わせられない"
  case "$n" in ''|*[!0-9]*) fail_open "run $id の job の件数を読めない ($n)" ;; esac
  [ "$n" -gt 0 ] || continue   # gate だけで終わった run は検証ではない
  echo "ci_schedule_gate: $sha は run $id で検証済み ($concl)。本体は走らせない" >&2
  echo run=false
  echo "prior=$concl"
  exit 0
done <<< "$ids"
echo run=true
