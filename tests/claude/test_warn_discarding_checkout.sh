#!/usr/bin/env bash
# _claude/hooks/warn-discarding-checkout.sh (PreToolUse(Bash): 未コミットの変更を捨てうる
# git checkout / restore の直前に注意を注入する) の unit テスト。
#
# なぜ: 規範 (_claude/rules/mutation-verify-new-tests.md の「復元の作法」) は発動点まで
# 名指しで書いてあるのに、2026-09-06 の 1 セッションで 3 回踏んだ。機械が最後の砦なので、
# 判定式の退行は「注意が出なくなる = 砦が消える」と同義。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/warn-discarding-checkout.sh"
# 本番の配線と同じ上限。timeout に殺されると hook は無出力で終わる = 注意が消えるので、
# 課さないと「ゲートが消える」退行を観測できない。
HOOK_TIMEOUT=10
# 🚨 「timeout が無いから skip」で緑を返さない (判定不能は合格ではない)
if command -v timeout >/dev/null 2>&1; then TIMEOUT_BIN=timeout
elif command -v gtimeout >/dev/null 2>&1; then TIMEOUT_BIN=gtimeout
else echo "✗ timeout(1) / gtimeout(1) がどちらも無い。本番と同じ上限を課せないので検査できない" >&2; exit 1; fi
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }

ok=0; fail=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT

# 偽 repo をケースごとに新規に作る (ケース間で状態を共有しない)
new_repo() { # → $REPO (x.go を 1 つ commit 済み)
  REPO=$(mktemp -d "$TMP_ROOT/repo.XXXXXX")
  ( cd "$REPO" && git init -q . && printf 'a\n' > x.go && git add -A &&
    git -c user.email=t@t -c user.name=t commit -qm init ) >/dev/null 2>&1
}
dirty() { printf 'b\n' >> "$REPO/x.go"; }

run() { # $1=command → hook の stdout
  jq -n --arg c "$1" --arg d "$REPO" '{cwd: $d, tool_input: {command: $c}}' |
    "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true
}

expect_warn() { # $1=説明 $2=command
  local out; out=$(run "$2")
  if [ -z "$out" ]; then echo "✗ 注意が出るべきなのに無出力: $1 — $2"; fail=$((fail+1)); return; fi
  # 🚨 「出力があった」で終わらせない: 実改行が混ざって JSON が壊れると Claude Code 側で
  #    捨てられる (無音の失敗)。jq に食わせて本文まで取り出せることを見る
  if ! printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | test("未コミットの変更")' >/dev/null 2>&1; then
    echo "✗ JSON が壊れている / 本文が注意になっていない: $1"; printf '%s\n' "$out" | head -3; fail=$((fail+1)); return
  fi
  # 🚨 permissionDecision を返さないこと。返すと「許可の判断を変えない」契約が壊れる
  if printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ permissionDecision を返している (deny/allow は契約外。注意だけを足す): $1"; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_silent() { # $1=説明 $2=command
  local out; out=$(run "$2")
  if [ -n "$out" ]; then echo "✗ 黙るべきなのに出力した: $1 — $2"; printf '%s\n' "$out" | head -2; fail=$((fail+1)); return; fi
  echo "✓ $1"; ok=$((ok+1))
}

echo "## 捨てる形 (dirty)"
for c in "git checkout -- x.go" "git checkout ." "git checkout -f" "git checkout --force main" \
         "git restore x.go" "git restore --staged --worktree x.go" \
         "git checkout HEAD -- x.go" "cd /tmp && git checkout -- x.go"; do
  new_repo; dirty; expect_warn "捨てる: $c" "$c"
done

echo "## 捨てない形 (dirty でも黙る)"
for c in "git checkout -b feature" "git checkout main" "git restore --staged x.go" \
         "git log --oneline" "make test" "git status" "git commit -m x"; do
  new_repo; dirty; expect_silent "捨てない: $c" "$c"
done

echo "## 変更が無ければ黙る (毎回出すとノイズになり読まれなくなる)"
new_repo; expect_silent "clean な repo で checkout --" "git checkout -- x.go"

echo "## untracked だけが dirty のときも出す (checkout では戻らない = 消えたら復元できない)"
new_repo; : > "$REPO/new.go"; expect_warn "untracked だけ" "git checkout -- x.go"

echo "## 異常系 (adversarial-review-own-safeguards §1)"
# repo でない場所 → 黙る (注意を出す根拠が無い)
REPO=$(mktemp -d "$TMP_ROOT/norepo.XXXXXX"); expect_silent "repo でない cwd" "git checkout -- x.go"
# cwd が実在しない → $PWD へ落ちる。落ちた先が dirty でも壊れない (JSON が出るか無出力のどちらか)
new_repo; dirty
out=$(jq -n --arg c "git checkout -- x.go" '{cwd: "/nonexistent/xxx", tool_input: {command: $c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if [ -z "$out" ] || printf '%s' "$out" | jq -e . >/dev/null 2>&1; then
  echo "✓ cwd が実在しなくても壊れない"; ok=$((ok+1))
else echo "✗ cwd 不在で壊れた JSON を出した"; fail=$((fail+1)); fi
# jq が無い → 静かに諦める (誤って注意を出さない)
new_repo; dirty
shim=$(mktemp -d "$TMP_ROOT/nojq.XXXXXX")
out=$(jq -n --arg c "git checkout -- x.go" --arg d "$REPO" '{cwd: $d, tool_input: {command: $c}}' |
  PATH="$shim" "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if [ -z "$out" ]; then echo "✓ jq 不在では黙る"; ok=$((ok+1)); else echo "✗ jq 不在なのに出力した"; fail=$((fail+1)); fi
# 非 JSON / 空入力で異常終了しない
for bad in "" "not json"; do
  if printf '%s' "$bad" | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" >/dev/null 2>&1; then
    ok=$((ok+1)); else echo "✗ 不正入力で異常終了した: [$bad]"; fail=$((fail+1)); fi
done
# 🚨 巨大な heredoc で timeout に殺されないこと (殺されると無出力 = 注意が消える)
new_repo; dirty
big=$(head -c 90000 /dev/zero | tr '\0' 'x')
out=$(jq -n --arg c "git commit -F - <<'M'
$big
M" --arg d "$REPO" '{cwd: $d, tool_input: {command: $c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if [ -z "$out" ]; then echo "✓ 90KB の入力でも timeout に殺されず、対象外なので黙る"; ok=$((ok+1));
else echo "✗ 90KB の対象外コマンドで出力した"; fail=$((fail+1)); fi

echo "## 配線 (hook 本体が正しくても settings.json から消えれば防御はゼロ)"
if jq -e '[.hooks.PreToolUse[] | select(.matcher == "Bash") | .hooks[].command]
          | any(test("warn-discarding-checkout"))' "$ROOT_DIR/_claude/settings.json" >/dev/null 2>&1; then
  echo "✓ PreToolUse(Bash) に配線されている"; ok=$((ok+1))
else echo "✗ settings.json の PreToolUse(Bash) に配線されていない"; fail=$((fail+1)); fi

printf '検査 %s 件: ok=%s / fail=%s\n' "$((ok+fail))" "$ok" "$fail"
[ "$fail" -eq 0 ] || exit 1
