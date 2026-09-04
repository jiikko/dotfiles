#!/usr/bin/env bash
# _claude/hooks/next-claim-unshared.sh (UserPromptSubmit: global または group 内 next/ の
# 他マシンから見えない claim = 未コミット / commit 済みだが未 push を検出して
# 「push してよいか伺え」を注入する) の unit テスト。
#
# なぜ: この hook は「人が glogx の `n` で付けた claim が push されないまま寝る」事故を拾う
# 唯一の装置 (Go の rename は Bash を通らないので PostToolUse 側では見えない)。判定式が
# 壊れると **無言で発火しなくなる** = 防御がゼロに戻る。
# 規範: _claude/rules/claim-issue-in-next-and-push.md
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/next-claim-unshared.sh"
HOOK_TIMEOUT=10

# 本番と同じ上限を課せないなら「合格」ではなく「判定不能 = 失敗」にする
if command -v timeout >/dev/null 2>&1; then TIMEOUT_BIN=timeout
elif command -v gtimeout >/dev/null 2>&1; then TIMEOUT_BIN=gtimeout
else echo "✗ timeout(1) / gtimeout(1) がどちらも無い。本番と同じ上限を課せないので検査できない" >&2; exit 1; fi
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }

fail=0; ok=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT

# 偽 repo を毎ケース新規に作る (ケース間で状態を共有しない)。
# 🚨 本物の dotfiles では走らせない: issues/next/ は空だと git に載らないので新品
#    チェックアウトと CI に存在せず、判定式が壊れていなくても全件無出力になる
#    (test_next_claim_push.sh が同じ罠を CI run 33649890092 で踏んでいる)
new_repo() { # → $REPO
  REPO=$(mktemp -d "$TMP_ROOT/repo.XXXXXX")
  ( cd "$REPO" && git init -q . && mkdir -p issues/next issues/done && : > issues/186-x.md &&
    git add -A && git -c user.email=t@t -c user.name=t commit -qm init ) >/dev/null 2>&1
}
new_epic_repo() { # global next/ が無く、group 内 next/ だけがある → $REPO
  REPO=$(mktemp -d "$TMP_ROOT/epic-repo.XXXXXX")
  ( cd "$REPO" && git init -q . && mkdir -p issues/epic/foo/next issues/done &&
    : > issues/epic/foo/186-x.md &&
    git add -A && git -c user.email=t@t -c user.name=t commit -qm init ) >/dev/null 2>&1
}
run_hook() { ( cd "$REPO" && printf '{"prompt":"x"}' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null ) || true; }

expect_fire() { # $1=説明
  local out; out=$(run_hook)
  if [ -z "$out" ]; then echo "✗ 発火すべきなのに無出力: $1"; fail=$((fail+1)); return; fi
  # 🚨 「出力があった」で終わらせない: 実改行が混ざると JSON が壊れて Claude Code 側で
  #    捨てられる (無音の失敗)。jq に食わせて本文まで取り出せることを見る
  if ! printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | test("push してよいか")' >/dev/null 2>&1; then
    echo "✗ JSON が壊れている / 本文が伺いになっていない: $1"; printf '%s\n' "$out" | head -5; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_silent() { # $1=説明
  local out; out=$(run_hook)
  if [ -n "$out" ]; then echo "✗ 発火してはいけないのに出力: $1"; printf '%s\n' "$out" | head -3; fail=$((fail+1)); return; fi
  echo "✓ $1"; ok=$((ok+1))
}

echo "## 発火する形 (claim が未コミット)"
new_repo; ( cd "$REPO" && mv issues/186-x.md issues/next/ )
expect_fire "素の mv (glogx の n と同じ形: D + 未追跡)"

new_repo; ( cd "$REPO" && git mv issues/186-x.md issues/next/ )
expect_fire "git mv (staged rename)"

new_repo; ( cd "$REPO" && git mv issues/186-x.md issues/next/ &&
  git -c user.email=t@t -c user.name=t commit -qm claim && mv issues/next/186-x.md issues/ )
expect_fire "claim の解除 (next から出す) も伺う (解除が伝わらないと claim 済みに見え続ける)"

echo "## 発火しない形"
new_repo
expect_silent "clean な作業ツリー"

new_repo; ( cd "$REPO" && printf 'x\n' >> issues/186-x.md )
expect_silent "issues/ の無関係な変更 (claim ではない)"

new_repo; ( cd "$REPO" && : > src-file.txt )
expect_silent "issues/ の外の変更"

new_repo; ( cd "$REPO" && git mv issues/186-x.md issues/next/ &&
  git -c user.email=t@t -c user.name=t commit -qm claim )
# 🚨 **commit しただけでは claim になっていない** (他マシンからは「未着手の issue」に見える)。
# issue 249 の項目 4 でここを拾うようにした。以前は「commit 済みなら黙る」が契約だった
expect_fire "claim が commit 済みだが未 push"

echo "## 発火する形 (group 内 next/ の claim)"
new_epic_repo; ( cd "$REPO" && mv issues/epic/foo/186-x.md issues/epic/foo/next/ )
expect_fire "epic group の未コミット claim"

new_epic_repo; ( cd "$REPO" && git mv issues/epic/foo/186-x.md issues/epic/foo/next/ )
expect_fire "epic group の staged claim"

new_epic_repo; ( cd "$REPO" && git mv issues/epic/foo/186-x.md issues/epic/foo/next/ &&
  git -c user.email=t@t -c user.name=t commit -qm claim )
expect_fire "epic group の commit 済みだが未 push の claim"

# push 済みなら黙る (claim が成立しているので急かす理由が無い)。
# 🚨 偽の remote を作って本当に push する — 「remote が無い repo」では未 push か push 済みかを
#    区別できず、この 2 ケースが同じ入力になってしまう
new_repo
bare=$(mktemp -d "$TMP_ROOT/bare.XXXXXX")
( cd "$bare" && git init -q --bare . ) >/dev/null 2>&1
( cd "$REPO" && git mv issues/186-x.md issues/next/ &&
  git -c user.email=t@t -c user.name=t commit -qm claim &&
  git remote add origin "$bare" && git push -q origin HEAD ) >/dev/null 2>&1
expect_silent "claim が push 済み (claim が成立しているので黙る)"

# opt-in の範囲: global / group 内の next/ が無い repo では規律ごと無効。
# 🚨 fixture は **検出条件を満たしたうえで opt-in だけが外れている形**にする。別名の dir
#    (issues/next2/) では検出の grep が最初から当たらず、opt-in を外す変異が素通りする
#    (変異検証 2026-09-03 で実際に素通りした)。claim を commit した後に issues/next/ を
#    まるごと消した形なら、status に issues/next/ の削除行が出つつ dir は無い = 狙いの形
scope=$(mktemp -d "$TMP_ROOT/noopt.XXXXXX")
( cd "$scope" && git init -q . && mkdir -p issues/next && : > issues/1-x.md && git add -A &&
  git -c user.email=t@t -c user.name=t commit -qm init &&
  git mv issues/1-x.md issues/next/ &&
  git -c user.email=t@t -c user.name=t commit -qm claim &&
  rm -rf issues/next ) >/dev/null 2>&1
out=$( cd "$scope" && printf '{"prompt":"x"}' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true )
if [ -n "$out" ]; then echo "✗ issues/next/ を消した repo (opt-out) で発火した"; fail=$((fail+1));
else echo "✓ issues/next/ が無い repo では発火しない (opt-out した repo を急かさない)"; ok=$((ok+1)); fi

echo "## 配線 (settings.json に載っているか)"
if jq -e '[.hooks.UserPromptSubmit[].hooks[].command] | any(endswith("next-claim-unshared.sh"))' \
     "$ROOT_DIR/_claude/settings.json" >/dev/null 2>&1; then
  echo "✓ UserPromptSubmit に配線されている"; ok=$((ok+1))
else
  echo "✗ settings.json の UserPromptSubmit に配線されていない (hook が動かない)"; fail=$((fail+1))
fi

printf '検査 %s 件: ok=%s / fail=%s\n' "$((ok+fail))" "$ok" "$fail"
[ "$fail" -eq 0 ] || exit 1
