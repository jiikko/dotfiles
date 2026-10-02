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

# body_of <hook の stdout>: 本文 (ask なら確認に出る理由、注意なら additionalContext)。JSON が壊れていれば空
body_of() { printf '%s' "$1" | jq -r '.hookSpecificOutput | (.permissionDecisionReason // .additionalContext // "")' 2>/dev/null || true; }

expect_warn() { # $1=説明 $2=command  曖昧な形: 注意だけを足し、許可の判断を変えない
  local out; out=$(run "$2")
  if [ -z "$out" ]; then echo "✗ 注意が出るべきなのに無出力: $1 — $2"; fail=$((fail+1)); return; fi
  # 🚨 「出力があった」で終わらせない: 実改行が混ざって JSON が壊れると Claude Code 側で
  #    捨てられる (無音の失敗)。jq に食わせて本文まで取り出せることを見る
  if ! printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | test("未コミットの変更")' >/dev/null 2>&1; then
    echo "✗ JSON が壊れている / 本文が注意になっていない: $1"; printf '%s\n' "$out" | head -3; fail=$((fail+1)); return
  fi
  # 🚨 曖昧な形 (ブランチの切り替えかもしれない) では permissionDecision を返さない。返すと偽陽性のたびに人が止まる
  if printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ 曖昧な形で permissionDecision を返している (注意だけを足す): $1"; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_ask() { # $1=説明 $2=command  確実に捨てる形: ask で人に選ばせ、理由に消える一覧を載せる (issue 623)
  local out; out=$(run "$2")
  if [ -z "$out" ]; then echo "✗ 確認を出すべきなのに無出力: $1 — $2"; fail=$((fail+1)); return; fi
  if ! printf '%s' "$out" | jq -e '.hookSpecificOutput | .permissionDecision == "ask" and (.permissionDecisionReason | test("未コミットの変更"))' >/dev/null 2>&1; then
    echo "✗ ask になっていない / 理由に一覧が無い / JSON が壊れている: $1"; printf '%s\n' "$out" | head -3; fail=$((fail+1)); return
  fi
  # ask でもモデルには同じ本文を渡し (人が許可・拒否した後に何が消えるかを知るため)、拒否された後に別の経路で消さないよう添える
  if ! printf '%s' "$out" | jq -e '.hookSpecificOutput | (.additionalContext | test("未コミットの変更")) and (.permissionDecisionReason | test("別のコマンド"))' >/dev/null 2>&1; then
    echo "✗ ask なのにモデルへの本文が無い / 拒否された後の案内が無い: $1"; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_no_ask() { # $1=説明 $2=command  ask にならない (黙るか注意だけ)。人もコマンドも止めない
  local out; out=$(run "$2")
  if [ -n "$out" ] && printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ ask / 許可の判断を返した (止めてはいけない形): $1"; fail=$((fail+1)); return
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
         "git checkout HEAD -- x.go" "git checkout -fq" "git checkout -qf main" \
         "(git checkout -- x.go)" \
         "git checkout -p" "git checkout --patch" \
         "git --no-pager checkout -- x.go" \
         "git  checkout -- x.go" "git restore --staged -W x.go" "git restore -SW x.go"; do
  new_repo; dirty; expect_ask "確実に捨てる (ask): $c" "$c"
done
# 曖昧な形 (ファイルを捨てるのかブランチの切り替えか、静的に区別できない) は注意だけ
for c in "git checkout x.go" "git checkout main"; do
  new_repo; dirty; expect_warn "曖昧 (注意だけ): $c" "$c"
done
# 複数の segment を持つコマンドは、確実な形を含んでも注意だけ (ask はコマンド全体が 1 つの git コマンドのとき)
for c in "git checkout main; git checkout -- x.go" "git status && git checkout -- x.go" "git checkout -- x.go
git status"; do
  new_repo; dirty; expect_warn "複数の segment (注意だけ): ${c%%$'\n'*}" "$c"
done
# 🚨 ask は単純なコマンド (引用符・heredoc・コメント・コマンド置換・行継続が無い) だけ。それ以外は確実な形を含んでも注意だけ
#    (ask にすると commit message や issue の本文で人とコマンドが止まり、`claude -p` では commit ごと拒否される。issue 623 の敵対的レビュー 2 周)
for c in "echo 'note: git checkout -- x.go'" "gh issue comment 1 --body 'a; git restore x.go'" \
         "git commit -F - <<'M'
fix: git checkout -- x.go を ask にする
M" "git commit -F - <<'MSG-EOF'
x; git checkout -- x.go
MSG-EOF" "git commit -m \"wip; git checkout -- x.go\" -- a.go" "git checkout main  # -f は付けない" \
         "# git checkout -- x.go はしない
git checkout main" "git checkout \\
  -- x.go"; do
  new_repo; dirty; expect_warn "単純でない形 (注意だけ): ${c%%$'\n'*}" "$c"
done
new_repo; dirty; expect_no_ask "コメントにしか無ければ止めない" "# git checkout -- x.go はしない
git status"
# `$(git` は git として拾わない (前置きの VAR=$(git rev-parse) を拾って黙る退行を避ける。旧と同じく黙る)。help も止めない
for c in "x=\$(git checkout -- x.go)" "out=\"\$(git checkout -- x.go)\"" "git restore -h" "git checkout --help"; do
  new_repo; dirty; expect_no_ask "止めない: $c" "$c"
done
# -h / --help は `--` より前にあれば help (`--` の後ろの -h はパスなので黙らない。4・5 周目)
new_repo; dirty; expect_ask "-- の後ろの -h では黙らない" "git checkout -- -h x.go"
for c in "git checkout -f -h" "git restore -W --help"; do new_repo; dirty; expect_no_ask "help は止めない: $c" "$c"; done
# 短いオプションの束ねは、引数を取る b / B (checkout) ・ s (restore) より前の文字だけを見る (後ろは名前・値。5 周目)
for c in "git checkout -bfix" "git checkout -qbperf"; do new_repo; dirty; expect_warn "ブランチ名の f / p は確実な形にしない: $c" "$c"; done
new_repo; dirty; expect_ask "-fbx は f があるので確実に捨てる" "git checkout -fbx"
new_repo; dirty; expect_no_ask "restore --staged -sWIP の値の W を -W と読まない" "git restore --staged -sWIP x.go"
# index にだけ変更がある (作業ツリーは index と同じ) なら、index から戻す形では消えないので ask にしない (5 周目)
new_repo; dirty; git -C "$REPO" add x.go; expect_warn "index にだけ変更 (注意だけ)" "git checkout -- x.go"
new_repo; dirty; expect_warn "前置きの VAR=\$(git …) があっても後ろの checkout を拾う (注意)" "TOP=\$(git rev-parse --show-toplevel) git checkout -- x.go"
# repo を差し替える指定があると対象に自信が持てないので注意だけ (同じ repo を指していても)
new_repo; dirty; expect_warn "--git-dir / --work-tree つき (注意だけ)" "git --git-dir=.git --work-tree=. checkout -- x.go"
# git が segment の先頭の語でなければ ask にしない (引数の中の字句)。前置きの VAR=値 は先頭に数えない
for c in "echo git checkout -- x.go" "time git checkout -- x.go"; do new_repo; dirty; expect_warn "git が先頭でない (注意だけ): $c" "$c"; done
new_repo; dirty; expect_ask "前置きの VAR=値 の後の git は先頭" "GIT_PAGER=cat git checkout -- x.go"
# 1 つの segment の最初の git だけを見るので、-m の中は注意も出ない (止めないことだけを固定する)
new_repo; dirty; expect_no_ask "commit -m の中の checkout は止めない" "git commit -m \"fix: git checkout -- x.go を ask にする\""

echo "## 捨てない形 (dirty でも黙る)"
for c in "git checkout -b feature" "git checkout -B feature" "git checkout --orphan fresh" \
         "git restore --staged x.go" "git restore -S x.go" \
         "git log --oneline" "make test" "git status" "git commit -m x" "git checkout"; do
  new_repo; dirty; expect_silent "捨てない: $c" "$c"
done

echo "## 変更が無ければ黙る (毎回出すとノイズになり読まれなくなる)"
new_repo; expect_silent "clean な repo で checkout --" "git checkout -- x.go" skip-control

echo "## untracked だけが dirty のときも出す (checkout では戻らない = 消えたら復元できない)"
# untracked は checkout / restore で消えないので、untracked だけなら ask にはせず注意だけ (失いようが無い状態で人を止めない。4 周目 P3-4)
new_repo; : > "$REPO/new.go"; expect_warn "untracked だけ (注意だけ)" "git checkout -- x.go"

echo "## cross-repo: 見るのは「捨てられる側」であって cwd ではない"
# 🚨 この repo の規範 (commit-with-pathspec.md) は「本体への操作は `git -C <本体の絶対パス>` で
# 対象を明示する」と推奨している。初版は cwd 側の dirty を見ていたので、**推奨形がそのまま盲点**
# になっていた (敵対的レビュー P2-1)。両方向を固定する。
new_repo; CLEAN="$REPO"          # clean な repo を cwd に
new_repo; dirty; DIRTY="$REPO"   # dirty な repo を -C の対象に
out=$(jq -n --arg c "git -C $DIRTY checkout -- x.go" --arg d "$CLEAN" '{cwd:$d,tool_input:{command:$c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if body_of "$out" | grep -qF "$DIRTY"; then
  echo "✓ clean な cwd から dirty な repo を触ると、対象 repo の変更を出す"; ok=$((ok+1))
else echo "✗ cwd が clean だと沈黙した (今まさに消える場面で黙る)"; fail=$((fail+1)); fi
out=$(jq -n --arg c "git -C $CLEAN checkout -- x.go" --arg d "$DIRTY" '{cwd:$d,tool_input:{command:$c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if [ -z "$out" ]; then echo "✓ dirty な cwd から clean な repo を触っても黙る (無関係な一覧を見せない)"; ok=$((ok+1))
else echo "✗ 対象は clean なのに cwd 側の変更を「消えるもの」として見せた"; printf '%s\n' "$out" | head -2; fail=$((fail+1)); fi
# 対象の repo の書き方 (issue 623 の敵対的レビュー P2-5 / P3-1): 引用符つきの -C / ~ / -c k=v の後の -C / cd <dir> && / サブシェル。
# どれも clean な cwd から dirty な repo を触る形で、対象 repo の一覧が出ること (= 黙らない) を見る
for c in "git -C \"$DIRTY\" checkout -- x.go" "git -c a=b -C $DIRTY checkout -- x.go" \
         "git -C ~/${DIRTY##*/} checkout -- x.go" "git -C \$HOME/${DIRTY##*/} checkout -- x.go"; do
  out=$(jq -n --arg c "$c" --arg d "$CLEAN" '{cwd:$d,tool_input:{command:$c}}' |
    HOME="${DIRTY%/*}" "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
  if body_of "$out" | grep -qF "$DIRTY"; then echo "✓ 対象 repo を読む: $c"; ok=$((ok+1))
  else echo "✗ 対象 repo を読めずに黙った / 違う repo を見た: $c"; printf '%s\n' "$out" | head -2; fail=$((fail+1)); fi
done
# 🚨 後ろの segment に最初の破棄の対象を奪われて黙らない (対象は最初の「捨てる segment」で決める。敵対的レビュー 3 周目 P2-1)。
#    dirty な cwd での確実な破棄が、旧と同じく少なくとも注意を出すこと (cd があるので ask にはしない)
for c in "git checkout -- x.go; cd $CLEAN; git checkout -- x.go" "git checkout -- x.go && cd $CLEAN && git checkout main" \
         "git commit -m 'x; cd /tmp'; git checkout -- x.go" "git checkout -- x.go; git -C $CLEAN checkout -b new"; do
  out=$(jq -n --arg c "$c" --arg d "$DIRTY" '{cwd:$d,tool_input:{command:$c}}' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
  if body_of "$out" | grep -qF "$DIRTY"; then echo "✓ 後ろの segment に対象を奪われない: $c"; ok=$((ok+1))
  else echo "✗ 後ろの segment に対象を奪われて黙った / 違う repo を見た: $c"; printf '%s\n' "$out" | head -2; fail=$((fail+1)); fi
done
# 🚨 対象の repo は segment ごとに持つ (敵対的レビュー 4 周目 P2-1 / P2-2)。
#    -C の無い破棄の後ろの `-C <dirty>` の破棄を見落とさない (clean な cwd でも dirty な対象の一覧を出す)
for c in "git restore x.go && git -C $DIRTY restore x.go" "git checkout -- x.go; git -C $DIRTY checkout -- x.go" \
         "git checkout main && git -C $DIRTY checkout -- x.go"; do
  out=$(jq -n --arg c "$c" --arg d "$CLEAN" '{cwd:$d,tool_input:{command:$c}}' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
  if body_of "$out" | grep -qF "$DIRTY"; then echo "✓ 後ろの -C <dirty> を見落とさない: $c"; ok=$((ok+1))
  else echo "✗ 後ろの -C <dirty> を見落として黙った: $c"; printf '%s\n' "$out" | head -2; fail=$((fail+1)); fi
done
#    確実な破棄が clean な別の repo を指していれば、cwd (dirty) の一覧で ask にしない (cwd 側の曖昧な形は注意だけ)
for c in "git checkout main && git -C $CLEAN restore x.go" "echo git checkout foo; git -C $CLEAN checkout -- x.go"; do
  out=$(jq -n --arg c "$c" --arg d "$DIRTY" '{cwd:$d,tool_input:{command:$c}}' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
  if [ -n "$out" ] && printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ clean な別の repo への破棄なのに cwd の一覧で ask にした: $c"; fail=$((fail+1))
  else echo "✓ 確実な破棄の対象が clean なら ask にしない: $c"; ok=$((ok+1)); fi
done
# 🚨 対象の repo に自信が無い形は ask にしない (cwd に倒した一覧で人を止めない。敵対的レビュー 3 周目 P2-2)
for c in "(cd $CLEAN && git checkout -- x.go)" "pushd $CLEAN; git checkout -- x.go" "cd $CLEAN && git checkout -- x.go" \
         "GIT_DIR=$CLEAN/.git GIT_WORK_TREE=$CLEAN git checkout -- x.go" "git --git-dir=$CLEAN/.git --work-tree=$CLEAN checkout -- x.go" \
         "git -C $DIRTY -C $CLEAN checkout -- x.go" "git -C /nonexistent/xyz checkout -- x.go" \
         "git status;	cd $CLEAN && git checkout -- x.go" "git checkout feature 2> >(tee -p log)"; do
  out=$(jq -n --arg c "$c" --arg d "$DIRTY" '{cwd:$d,tool_input:{command:$c}}' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
  if [ -n "$out" ] && printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ 対象に自信が無いのに ask にした: $c"; fail=$((fail+1))
  else echo "✓ 対象に自信が無ければ ask にしない: $c"; ok=$((ok+1)); fi
done

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
if [ -n "$(body_of "$out")" ]; then
  echo "✓ 90KB の入力でも本走査を通り切り、timeout に殺されない"; ok=$((ok+1))
else echo "✗ 90KB の入力で注意が出なかった (timeout に殺された可能性)"; fail=$((fail+1)); fi

echo "## 判定不能を緑に畳まない (adversarial-review-own-safeguards §2)"
# 🚨 「検査できなかった」は「変更なし」ではない。rev-parse は通るが status だけ落ちる状態
# (index の破損) を作って、黙らずに判定不能を出すことを固定する。
# 実測 2026-09-06: 初版はここで exit 0 しており、**直上のコメントが約束したことを実装が
# やっていなかった** (敵対的レビューが同じ手順で発見)。
new_repo; dirty
printf 'GARBAGE' > "$REPO/.git/index"
if git -C "$REPO" rev-parse --show-toplevel >/dev/null 2>&1 &&
   ! git -C "$REPO" status --porcelain >/dev/null 2>&1; then
  out=$(run "git checkout -- x.go")
  if body_of "$out" | grep -q "判定できなかった" &&
     printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision == "ask"' >/dev/null 2>&1; then
    echo "✓ status が失敗したら判定不能として出す (確実な形なので ask。黙らない)"; ok=$((ok+1))
  else echo "✗ status 失敗を黙って握り潰した (沈黙 = 成功になっている)"; fail=$((fail+1)); fi
else
  # 🚨 前提を作れないなら合格でも不合格でもなく**判定不能 = 失敗**として扱う
  echo "✗ 前提を作れなかった (index を壊しても status が落ちない)。この検査は判定不能"; fail=$((fail+1))
fi

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
