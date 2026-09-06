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
expect_silent() { # $1=説明 $2=command  [$3=skip-control なら対の確認を省く]
  # 🚨 対の positive control: 同じ repo 状態で「必ず出る形」が出ることを確かめてから
  # 「黙った」を受け入れる。clean / repo でない / jq 不在のケースは control 自体が
  # 正しく出ないので、そこだけ省く (省いた分は上の生存確認が受け持つ)。
  if [ "${3:-}" != "skip-control" ] && [ -z "$(run 'git checkout -- x.go')" ]; then
    echo "✗ 対の control が出ない (hook が死んでいる可能性): $1"; fail=$((fail+1)); return
  fi
  local out; out=$(run "$2")
  if [ -n "$out" ]; then echo "✗ 黙るべきなのに出力した: $1 — $2"; printf '%s\n' "$out" | head -2; fail=$((fail+1)); return; fi
  echo "✓ $1"; ok=$((ok+1))
}

# 🚨 **生存確認を最初に置く** (敵対的レビュー P1-2)。expect_silent は「出力が空」しか見ておらず、
# **hook が壊れて死んでも空**なので、「正しく黙った」と「砦が消えた」が同じ観測になる。
# 実測: hook の先頭に `exit 0` を差す変異 (= 何もしない hook) で、24 件中 **15 件が緑のまま**
# 通った (緑で残ったのは expect_silent 系すべてと配線)。ここで先に「必ず出る形」を確かめ、
# 出なければ以降の緑に意味が無いので即失敗させる。
new_repo; dirty
if [ -z "$(run 'git checkout -- x.go')" ]; then
  echo "✗ 生存確認に失敗: 未コミットがある状態の 'git checkout -- x.go' で注意が出ない。"
  echo "  hook が死んでいる / 配線が外れている / 判定が壊れている。以降の「黙る」検査は"
  echo "  すべて無意味なので、ここで止める。"
  exit 1
fi
echo "✓ 生存確認: 既知の「必ず出る形」で注意が出る"
ok=$((ok+1))

echo "## 捨てる形 (dirty)"
# 🚨 `git checkout <何か>` は出す側へ倒す。`git checkout foo.go` (捨てる) と
#    `git checkout foo` (ブランチ切替) は**静的に区別できない**ので、宣言したバイアス
#    「取りこぼすより出す側へ倒す」に従って両方出す。初版はブランチ形を黙らせており、
#    そのせいで `git checkout x.go` (`--` なし) が沈黙していた (敵対的レビュー P2-5)。
for c in "git checkout -- x.go" "git checkout ." "git checkout -f" "git checkout --force main" \
         "git restore x.go" "git restore --staged --worktree x.go" \
         "git checkout HEAD -- x.go" "cd /tmp && git checkout -- x.go" \
         "git checkout x.go" "git checkout main" "git checkout -p" \
         "git --no-pager checkout -- x.go" "git --git-dir=.git --work-tree=. checkout -- x.go" \
         "git  checkout -- x.go" "git restore --staged -W x.go" "git restore -SW x.go"; do
  new_repo; dirty; expect_warn "捨てる: $c" "$c"
done

echo "## 捨てない形 (dirty でも黙る)"
for c in "git checkout -b feature" "git checkout -B feature" "git checkout --orphan fresh" \
         "git restore --staged x.go" "git restore -S x.go" \
         "git log --oneline" "make test" "git status" "git commit -m x" "git checkout"; do
  new_repo; dirty; expect_silent "捨てない: $c" "$c"
done

echo "## 変更が無ければ黙る (毎回出すとノイズになり読まれなくなる)"
new_repo; expect_silent "clean な repo で checkout --" "git checkout -- x.go" skip-control

echo "## untracked だけが dirty のときも出す (checkout では戻らない = 消えたら復元できない)"
new_repo; : > "$REPO/new.go"; expect_warn "untracked だけ" "git checkout -- x.go"

echo "## cross-repo: 見るのは「捨てられる側」であって cwd ではない"
# 🚨 この repo の規範 (commit-with-pathspec.md) は「本体への操作は `git -C <本体の絶対パス>` で
# 対象を明示する」と推奨している。初版は cwd 側の dirty を見ていたので、**推奨形がそのまま盲点**
# になっていた (敵対的レビュー P2-1)。両方向を固定する。
new_repo; CLEAN="$REPO"          # clean な repo を cwd に
new_repo; dirty; DIRTY="$REPO"   # dirty な repo を -C の対象に
out=$(jq -n --arg c "git -C $DIRTY checkout -- x.go" --arg d "$CLEAN" '{cwd:$d,tool_input:{command:$c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if printf '%s' "$out" | jq -e --arg d "$DIRTY" '.hookSpecificOutput.additionalContext | contains($d)' >/dev/null 2>&1; then
  echo "✓ clean な cwd から dirty な repo を触ると、対象 repo の変更を出す"; ok=$((ok+1))
else echo "✗ cwd が clean だと沈黙した (今まさに消える場面で黙る)"; fail=$((fail+1)); fi
out=$(jq -n --arg c "git -C $CLEAN checkout -- x.go" --arg d "$DIRTY" '{cwd:$d,tool_input:{command:$c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if [ -z "$out" ]; then echo "✓ dirty な cwd から clean な repo を触っても黙る (無関係な一覧を見せない)"; ok=$((ok+1))
else echo "✗ 対象は clean なのに cwd 側の変更を「消えるもの」として見せた"; printf '%s\n' "$out" | head -2; fail=$((fail+1)); fi

echo "## 異常系 (adversarial-review-own-safeguards §1)"
# repo でない場所 → 黙る (注意を出す根拠が無い)
REPO=$(mktemp -d "$TMP_ROOT/norepo.XXXXXX"); expect_silent "repo でない cwd" "git checkout -- x.go" skip-control
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
# 🚨 fixture に checkout を混ぜる (敵対的レビュー P3-1)。含まないと性能ガードの case で
# 即 exit 0 し、**ループに 1 度も入らない** = 何も測っていない vacuous なテストになる。
big=$(head -c 90000 /dev/zero | tr '\0' 'x')
big="git checkout -- $big"
out=$(jq -n --arg c "git commit -F - <<'M'
$big
M" --arg d "$REPO" '{cwd: $d, tool_input: {command: $c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
# 🚨 期待は「時間内に**注意が出る**」。timeout に殺されると stdout は 0 byte になるので、
# 「出た」ことが本走査を最後まで通った証拠になる (無出力だと殺されたのか対象外なのか
# 区別できない = 沈黙が成功に見える形)。
if printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext' >/dev/null 2>&1; then
  echo "✓ 90KB の入力でも本走査を通り切り、timeout に殺されない"; ok=$((ok+1))
else echo "✗ 90KB の入力で注意が出なかった (timeout に殺された可能性)"; fail=$((fail+1)); fi

echo "## 配線 (hook 本体が正しくても settings.json から消えれば防御はゼロ)"
# 🚨 **これが緑でも「本番で armed」ではない** (敵対的レビュー P1-3)。ここが読むのは
# **このツリーの** settings.json で、本番の hook は `~/dotfiles/_claude/hooks/...` という
# 実体パスから起動する。worktree のコピーはどこからも読まれないので、統合は
# `push` → `git -C ~/dotfiles pull --rebase` まで通して初めて効く
# (.claude/rules/worktree-per-session.md「push は反映ではない」)。
if jq -e '[.hooks.PreToolUse[] | select(.matcher == "Bash") | .hooks[].command]
          | any(test("warn-discarding-checkout"))' "$ROOT_DIR/_claude/settings.json" >/dev/null 2>&1; then
  echo "✓ PreToolUse(Bash) に配線の記述がある"; ok=$((ok+1))
else echo "✗ settings.json の PreToolUse(Bash) に配線されていない"; fail=$((fail+1)); fi
# 配線が指すファイルが実在するか (改名すると記述だけ残って無音で死ぬ)
wired=$(jq -r '[.hooks.PreToolUse[] | select(.matcher == "Bash") | .hooks[].command]
               | map(select(test("warn-discarding-checkout")))[0] // ""' "$ROOT_DIR/_claude/settings.json")
wired_rel="${wired#*_claude/}"
if [ -n "$wired" ] && [ -x "$ROOT_DIR/_claude/$wired_rel" ]; then
  echo "✓ 配線が指すファイルが実在して実行可能"; ok=$((ok+1))
else echo "✗ 配線が指すファイルが無い / 実行できない: $wired"; fail=$((fail+1)); fi

printf '検査 %s 件: ok=%s / fail=%s\n' "$((ok+fail))" "$ok" "$fail"
[ "$fail" -eq 0 ] || exit 1
