#!/usr/bin/env bash
# scripts/ci_schedule_gate.sh の判断を確かめる。本物の GitHub に問い合わせないよう、偽の gh を PATH の先頭に置く。
# 偽の gh は、渡された --jq を本物の jq で当てる (jq の式の誤りも検出する)。返す JSON は FAKE_RUNS で決める。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/ci_schedule_gate.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
command -v jq >/dev/null || { echo "✗ jq が無い (偽の gh が --jq を当てるのに要る)" >&2; exit 1; }

fail=0
note() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fail=1; }

mkdir -p "$TMP_DIR/fakebin"
cat > "$TMP_DIR/fakebin/gh" <<'GH'
#!/usr/bin/env bash
# gh api <path> --jq <式>。path が runs/<id>/jobs なら FAKE_JOBS の <id> の job を、それ以外は FAKE_RUNS を返す。
# 本物の --jq と同じく文字列は引用符なしで出す (jq -r)
echo "$2" >> "$FAKE_LOG"
if [[ "$2" =~ /runs/([0-9]+)/jobs ]]; then
  [ -z "${FAKE_FAIL_JOBS:-}" ] || { echo "HTTP 502" >&2; exit 1; }
  printf '%s' "${FAKE_JOBS:-{\}}" | jq "{jobs: (.[\"${BASH_REMATCH[1]}\"] // [])}" | jq -r "$4"
else
  [ -z "${FAKE_FAIL:-}" ] || { echo "HTTP 502" >&2; exit 1; }
  printf '%s' "$FAKE_RUNS" | jq -r "$4"
fi
GH
chmod +x "$TMP_DIR/fakebin/gh"

# gate <runs> [<jobs>]: runs は "id:conclusion:時刻" の並び、jobs は '{"<id>":[{"name":…,"conclusion":…}]}'。この run の id は 9
gate() {
  local out='{"workflow_runs":[' sep='' id co at
  for r in $1; do
    IFS=: read -r id co at <<< "$r"
    out="$out$sep{\"id\":$id,\"status\":\"completed\",\"conclusion\":\"$co\",\"created_at\":\"2026-10-30T0${at}:00:00Z\"}"; sep=','
  done
  : > "$TMP_DIR/path"
  FAKE_RUNS="$out]}" FAKE_JOBS="${2:-}" FAKE_LOG="$TMP_DIR/path" PATH="$TMP_DIR/fakebin:$PATH" \
    "$SCRIPT" tests.yml abc123 o/r 9 2>"$TMP_DIR/err" | paste -sd' ' -
}
body_ok='[{"name":"schedule-gate","conclusion":"success"},{"name":"dotfiles-tests (rest)","conclusion":"success"}]'
body_ng='[{"name":"dotfiles-tests (rest)","conclusion":"failure"}]'
gate_only='[{"name":"schedule-gate","conclusion":"success"},{"name":"dotfiles-tests (rest)","conclusion":"skipped"}]'

check() {  # $1 = 説明 $2 = 期待 $3 = 実際
  if [ "$3" = "$2" ]; then note "$1"; else bad "$1 (want '$2', got '$3')"; fi
}
check "run が無いなら本体を走らせる" "run=true" "$(gate "")"
check "本体が緑で終えた run があれば走らせず、緑を引き継ぐ" "run=false prior=success" "$(gate "1:success:1" "{\"1\":$body_ok}")"
check "本体が赤で終えた run があれば走らせず、赤を引き継ぐ" "run=false prior=failure" "$(gate "1:failure:1" "{\"1\":$body_ng}")"
check "gate だけで終わった run は検証に数えない" "run=true" "$(gate "1:success:1" "{\"1\":$gate_only}")"
check "gate だけの新しい run を飛ばし、本体が走った古い run を引き継ぐ" "run=false prior=success" \
  "$(gate "2:success:2 1:success:1" "{\"2\":$gate_only,\"1\":$body_ok}")"
check "本体が走った run が複数なら新しい方を引き継ぐ" "run=false prior=failure" \
  "$(gate "1:success:1 2:failure:2" "{\"1\":$body_ok,\"2\":$body_ng}")"
check "取り消された run は数えない" "run=true" "$(gate "1:cancelled:1" "{\"1\":$body_ok}")"
check "この run 自身は数えない" "run=true" "$(gate "9:success:1" "{\"9\":$body_ok}")"
grep -qF 'repos/o/r/actions/workflows/tests.yml/runs?head_sha=abc123&status=completed' "$TMP_DIR/path" &&
  note "問い合わせる run は workflow・sha・完了で絞る" || bad "問い合わせのパスが違う: $(cat "$TMP_DIR/path")"
check "run の問い合わせに失敗したら本体を走らせる" "run=true" "$(FAKE_FAIL=1 gate "1:success:1" "{\"1\":$body_ok}")"
grep -q '問い合わせられない' "$TMP_DIR/err" && note "問い合わせの失敗の理由を出す" || bad "問い合わせの失敗の理由を出さない"
check "job の問い合わせに失敗したら本体を走らせる" "run=true" "$(FAKE_FAIL_JOBS=1 gate "1:success:1" "{\"1\":$body_ok}")"

# workflow の配線: schedule の起動・push と別の concurrency の group・schedule-gate・本体の job がみな gate を通る。
# group から event_name が消えると schedule の run が次の push に取り消され、足した意味が無くなる (壊れても緑のまま)
for wf in tests.yml bench.yml; do
  f="$ROOT_DIR/.github/workflows/$wf"
  grep -qE "^  schedule:$" "$f" && grep -qE "^    - cron: '" "$f" || bad "$wf に schedule の起動が無い"
  grep -qF "group: \${{ github.workflow }}-\${{ github.ref }}-\${{ github.event_name == 'schedule' && 'schedule' || 'push' }}" "$f" ||
    bad "$wf の concurrency の group が schedule と push を分けていない"
  grep -qF "scripts/ci_schedule_gate.sh $wf " "$f" || bad "$wf の schedule-gate が自分の workflow のファイル名を渡していない"
  gatebody=$(awk '$0 == "  schedule-gate:" {f=1;next} f && /^  [a-z0-9_-]+:$/{exit} f' "$f")
  grep -qF "if: github.event_name == 'schedule'" <<< "$gatebody" || bad "$wf の schedule-gate が schedule の run だけに限っていない"
  failstep=$(awk "/if: steps.gate.outputs.prior == 'failure'/{f=1} f" <<< "$gatebody")
  grep -qF 'gh run cancel "${{ github.run_id }}"' <<< "$failstep" && grep -qE 'exit 1' <<< "$failstep" ||
    bad "$wf の schedule-gate が、赤の commit で gate だけの run を取り消さない / 取り消しが届かないときに赤で終えない"
  jobs=$(awk '/^jobs:/{j=1;next} j && /^  [a-z0-9_-]+:$/{sub(/:$/,"");sub(/^  /,"");print}' "$f")
  [ -n "$jobs" ] || bad "$wf の job を数えられない"
  for job in $jobs; do
    [ "$job" = schedule-gate ] && continue
    body=$(awk -v j="$job" '$0 == "  " j ":" {f=1;next} f && /^  [a-z0-9_-]+:$/{exit} f' "$f")
    grep -qF "if: \${{ !cancelled() && (github.event_name != 'schedule' || needs.schedule-gate.outputs.run != 'false') }}" <<< "$body" ||
      bad "$wf の $job の条件が違う (push で走る !cancelled()・gate が落ちても走る != 'false')"
  done
done
[ "$fail" -ne 0 ] || note "tests.yml と bench.yml の schedule の配線"

[ "$fail" -eq 0 ] || exit 1
printf '✓ ci_schedule_gate: 全ケース通過\n'
