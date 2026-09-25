#!/usr/bin/env bash
# shellcheck disable=SC2016 # 検査に渡すコマンドの文字は展開させずにそのまま書く ($? / $(...) / ${PIPESTATUS[0]} を含む)
# _claude/hooks/deny-piped-push-then-destroy.sh (PreToolUse(Bash): push の結果をパイプに通したまま worktree / branch を消す Bash を deny) の
# unit テスト。合成した hook JSON を stdin で流し、deny / allow の判定を pin する (issue 454)。
#
# なぜ: `git push … | tail -1 && git worktree remove …` は push が弾かれても削除が走り、未 push の commit を失う (retro 266 / 450 で 2 回)。
# 判定式の退行 = 事故の再発経路が開く。脅威モデルと「検出しない形」はフック冒頭が正本。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/deny-piped-push-then-destroy.sh"
# ロケールを UTF-8 に固定する (hook の上限を byte で数えることを見る検査は、C ロケールでは文字数 = byte になり退行が見えない)
# shellcheck source=tests/lib/utf8_locale.sh
. "$ROOT_DIR/tests/lib/utf8_locale.sh"
SETTINGS="$ROOT_DIR/_claude/settings.json"
HOOK_TIMEOUT=10 # 本番の配線 (settings.json) と同じ上限。殺されると無出力 = 素通り
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_BIN=timeout
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_BIN=gtimeout
else
  echo "✗ timeout(1) / gtimeout(1) がどちらも無い。本番と同じ上限を課せないので検査できない" >&2
  exit 1
fi
[ -x "$HOOK" ] || { printf '✗ フック本体が無い / 実行権限が無い: %s\n' "$HOOK"; exit 1; }
if ! command -v jq >/dev/null 2>&1; then
  # フック本体は jq 不在で fail-open (防御ゼロ) なので、ここを成功にしない
  printf '✗ jq が無いため判定を検査できない (フック自体も jq 不在時は無効)\n'
  exit 1
fi
jq -e '[.hooks.PreToolUse[]? | select((.matcher // "") | test("Bash")) | .hooks[]?.command // ""]
       | map(test("deny-piped-push-then-destroy")) | any' "$SETTINGS" > /dev/null \
  || { printf '✗ settings.json の PreToolUse(Bash) にフックが配線されていない (防御が丸ごと無効)\n'; exit 1; }
printf '✓ settings.json の PreToolUse(Bash) に配線されている\n'

fail=0
checked=0
# judge <command>: deny / allow / error:<rc> を出す
judge() {
  local out rc=0
  out=$(jq -n --arg c "$1" '{tool_name: "Bash", tool_input: {command: $c}}' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK") || rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "error:$rc"
  elif [ -z "$out" ]; then
    echo allow
  elif printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision == "deny"' > /dev/null 2>&1; then
    echo deny
  else
    echo "error:unexpected-output"
  fi
}
expect() { # expect <deny|allow> <説明> <command>
  local got
  got=$(judge "$3")
  checked=$((checked + 1))
  if [ "$got" = "$1" ]; then
    printf '✓ %s: %s\n' "$1" "$2"
  else
    printf '✗ %s を期待したが %s: %s\n    %s\n' "$1" "$got" "$2" "$3"
    fail=1
  fi
}

# --- deny: push の rc がパイプに隠れたまま削除が走る ---------------------------------
expect deny "実際に踏んだ形 (retro 450)" \
  'git fetch -q && git rebase -q origin/master && git push origin HEAD:master 2>&1 | tail -1 && git -C ~/dotfiles pull -q --rebase && git -C ~/dotfiles worktree remove --force ~/dotfiles-wt-x && echo removed'
expect deny "; で繋いだ形 (push の成否に関係なく走る)" \
  'git push origin HEAD:master | tail -2; git worktree remove --force ../wt'
expect deny "改行で繋いだ形" \
  $'git push origin HEAD:master 2>&1 | tail -1\ngit worktree remove ../wt'
expect deny "-C に引用符の値" \
  'git -C "$wt" push origin HEAD:master | cat && git -C "$root" worktree remove "$wt"'
expect deny "branch -D" \
  'git push -u origin feat | tail -1 && git branch -D feat'
expect deny "branch -df (まとめた短いフラグ)" \
  'git push | tail -1 && git branch -df feat'
expect deny "branch --delete" \
  'git push | tail -1 && git branch --delete feat'
expect deny "push をサブシェルで囲み、パイプがグループの外 (敵対的レビュー P1)" \
  '(cd ../wt && git push origin HEAD:master 2>&1) | tail -1 && git worktree remove ../wt'
expect deny "push を { …; } で囲み、パイプがグループの外" \
  '{ cd ../wt && git push origin HEAD:master; } 2>&1 | tail -2 && git worktree remove ../wt'
expect deny "alias 回避の \\git" \
  '\git push | tail -1 && \git worktree remove ../wt'
expect deny "pipefail がコメントの中にあるだけ (通す理由にしない)" \
  'git push | tail -1 && git worktree remove ../wt # pipefail'
expect deny "残したコマンド置換の中の ' #' で後段を落とさない (敵対的レビュー 2 周目)" \
  'r="$(git push 2>&1 | tail -1) #1" && git worktree remove ../wt'
expect deny "case の ) でグループの段を閉じない" \
  '(cd ../wt && case $b in main) git push ;; esac) | tail -1 && git worktree remove ../wt'
expect deny "引用符の外の \\' を引用符と読まない" \
  "echo it\\'s; git push | tail -1 && git worktree remove ../wt; echo don\\'t"
expect deny "空白の無い push>log 2>&1|" \
  'git push>log 2>&1| tail -1 && git worktree remove ../wt'
expect deny "算術の <<n を heredoc と読まない" \
  $'x=$((1<<n))\ngit push | tail -1 && git worktree remove ../wt'
expect deny "ブランチ名の中の pipefail で通さない" \
  'git push origin pipefail-fix | tail -1 && git branch -D pipefail-fix'
expect deny "branch -vD (実在の短いフラグとまとめた削除。敵対的レビュー 4 周目)" \
  'git push | tail -1 && git branch -vD wt'
expect deny "branch --d / --de (--delete の一意な先頭。敵対的レビュー 5 周目)" \
  'git push | tail -1 && git branch --d wt'
expect deny "branch --de" \
  'git push | tail -1 && git branch --de wt'
expect deny "branch -dv" \
  'git push | tail -1 && git branch -dv wt'
expect deny "branch --del (長いオプションの先頭だけ)" \
  'git push | tail -1 && git branch --del wt'
expect deny "<<- の本文と、タブの付いた区切り" \
  $'x <<-E\n\tls\n\tE\ngit push | tail -1 && git worktree remove ../wt'
expect deny "空白の無い git push|tail (敵対的レビュー 3 周目)" \
  'git push|tail -1 && git worktree remove ../wt'
expect deny "空白の無い git push|&tail" \
  'git push|&tail -1 && git worktree remove ../wt'
for r in '2>&3' '>&-' '>& log' '>&/dev/null' '2>& 1' '<&0' '2>&3 3>&1'; do
  expect deny "リダイレクト $r の & でパイプを切らない" "git push $r | tail -1 && git worktree remove ../wt"
done
expect deny "<<<WORD の後に WORD の行があっても heredoc と取り違えない" \
  $'tr a b <<<EOF\ngit push | tail -1 && git worktree remove ../wt\nEOF'
expect deny "<<< (here-string) を heredoc と取り違えない" \
  $'tr a b <<<hello\ngit push | tail -1 && git worktree remove ../wt'
expect deny "|& (stderr ごとパイプ)" \
  'git push origin x |& tail -1 && git worktree remove ../wt'
expect deny "bash -c の中身" \
  "bash -c 'git push origin HEAD:master | tail -1 && git worktree remove ../wt'"
expect deny "二重引用符の中のコマンド置換" \
  'r="$(git push origin HEAD:master 2>&1 | tail -1)" && git worktree remove ../wt'
expect deny "行継続" \
  $'git push origin HEAD:master \\\n  | tail -1 && git worktree remove ../wt'
expect deny "worktree rm (別名)" \
  'git push | tail -1 && git worktree rm ../wt'
expect deny "フルパスの git" \
  '/usr/bin/git push | tail -1 && /usr/bin/git worktree remove ../wt'
expect deny "|| で繋いだ形" \
  'git push | tail -1 || git worktree remove ../wt'

# --- allow: rc を正しく見ている / 削除が無い / 本文の文言 ----------------------------
expect allow "push の rc を変数に取ってから繋ぐ (推奨の形)" \
  'git push origin HEAD:master > "$S/push.log" 2>&1; rc=$?; tail -2 "$S/push.log"; [ $rc -eq 0 ] && git -C ~/dotfiles worktree remove --force ~/wt'
expect allow "set -o pipefail を先に" \
  'set -o pipefail; git push origin HEAD:master | tail -1 && git worktree remove ../wt'
expect allow "PIPESTATUS で push の rc を見る" \
  'git push | tee log; [ "${PIPESTATUS[0]}" -eq 0 ] && git worktree remove ../wt'
expect allow "パイプの無い && (rc は push のもの)" \
  'git push origin HEAD:master && git worktree remove ../wt'
expect allow "push をパイプに通すが削除は無い" \
  'git push origin HEAD:master 2>&1 | tail -1'
expect allow "削除が push より前" \
  'git worktree remove ../old && git push origin HEAD:master | tail -1'
expect allow "push を含まないグループのパイプ" \
  '(git status) | tail -1 && git worktree remove ../wt'
expect allow "push の後を && で繋ぎ、パイプは別のコマンド" \
  'git push origin HEAD:master && git log --oneline | head -3 && git worktree remove ../wt'
expect allow "パイプの付いたのが push でない" \
  'git push origin HEAD:master; git log --oneline | head -3 && git worktree remove ../wt'
expect allow "commit message の heredoc の本文に同じ文言" \
  $'git commit -F - <<\'M\'\nfix: git push | tail -1 && git worktree remove を止める\nM\ngit push origin HEAD:master'
# 分かっていて受ける偽陽性 (字面で見る代わり。フック冒頭)。commit message は heredoc で渡せば当たらない (上の heredoc のケース)
expect deny "引用符の中の文言 (分かっていて受ける偽陽性)" \
  'echo "git push | tail -1 && git worktree remove ../wt"'
expect deny "コメントの中の文言 (分かっていて受ける偽陽性)" \
  'git status # git push | tail -1 && git worktree remove ../wt'
expect allow "git branch の一覧 (削除でない)" \
  'git push | tail -1 && git branch -vv'
expect allow "背景実行の & はパイプを push から切る" \
  'git push & ls | tail -1 && git branch -d x'
expect allow "推奨の形を複数行にした (git の引数の連なりは改行をまたがない)" \
  $'git push origin HEAD:master > push.log 2>&1\nrc=$?\ntail -2 push.log | cat\n[ $rc -eq 0 ] && git worktree remove ../wt'
expect allow "前の行の git と次の行の push を 1 つのコマンドと読まない" \
  $'git status\necho push done | tee -a log\ngit branch -d feature'
expect allow "branch --sort -committerdate は削除でない" \
  'git push | tail; git branch --sort -committerdate'
expect allow "push の直後の ; で切る (境界の 1 文字)" \
  'git push; git log | tail -1 && git worktree remove ../wt'
expect allow "push の直後の & で切る" \
  'git push&&>log cat log|tail && git worktree remove ../wt'
expect allow "<<\\EOF の本文を落とす" \
  $'cat <<\\EOF\ngit push | tail && git worktree remove x\nEOF\necho ok'
expect allow "<<- の本文を落とす" \
  $'x <<-E\n\tgit push | tail && git worktree remove y\n\tE\nls'
expect allow "無関係なコマンド" \
  'ls -la | head'

# --- 大きい入力: timeout に殺されず、判定できる -------------------------------------
big=$'git commit -F - <<\'M\'\n'"$(printf 'git push | tail && git worktree remove の話 %.0s\n' $(seq 1 1500))"$'\nM\ngit push origin HEAD:master'
expect allow "大きい heredoc (約 100KB) でも上限内に判定する" "$big"

bigq="echo \"$(printf 'x%.0s' $(seq 1 60000))\"; git push origin x | tail -1 && git worktree remove ../wt"
expect deny "引用符の中身が大きい (60KB) 入力も、上限の時間内に判定する (素通りにしない)" "$bigq"

mb="echo \"$(python3 -c "print('𝕏' * 40000)")\"; git push origin x | tail -1 && git worktree remove ../wt"
# 文字数では上限 (131072) の内側・byte では外側 (約 160KB) の入力。走査に進んでも結局 deny になりうる (8 秒かかる) ので、deny の理由が「大きすぎて」(上限で諦めた) であることまで見る
checked=$((checked + 1))
mb_reason=$(jq -n --arg c "$mb" '{tool_input: {command: $c}}' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" | jq -r '.hookSpecificOutput.permissionDecisionReason // ""')
if [[ "$mb_reason" == *"大きすぎて"* ]]; then
  printf '✓ deny: 4 byte の文字の大きい入力も byte で数えて、上限で拒否に倒す (文字数で数えると上限が 4 倍に膨らむ)\n'
else
  printf '✗ 4 byte の文字の大きい入力を上限で止めていない (文字数で数えている?): %s\n' "${mb_reason:0:80}"
  fail=1
fi

many=$(for i in $(seq 1 1500); do printf 'x <<A%s\n' "$i"; done)
many="$many"$'\ngit push | tail -1 && git worktree remove ../wt'
expect deny "heredoc の開始が多い入力も、上限の時間内に判定を諦めて拒否に倒す" "$many"

manyl="git push 2>&1 | tail -1 && git worktree remove ../wt"$'\n'"$(for i in $(seq 1 99); do echo 'x=$((1<<n))'; done)"$'\n'"$(printf 'a\n%.0s' $(seq 1 60000))"
expect deny "区切りの見つからない << が多く行も多い入力 (約 120KB) も、上限の時間内に判定する (敵対的レビュー 4 周目)" "$manyl"

# awk が失敗したら判定できないので拒否に倒す (素通りにしない)。失敗する偽の awk を PATH の先頭に置く
fake=$(mktemp -d)
trap 'rm -rf "$fake"' EXIT
printf '#!/bin/sh\nexit 2\n' > "$fake/awk"
chmod +x "$fake/awk"
checked=$((checked + 1))
fa=$(jq -n --arg c 'git push 2>&1 | tail -1 && git worktree remove ../wt' '{tool_input: {command: $c}}' | PATH="$fake:$PATH" "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" | jq -r '.hookSpecificOutput.permissionDecisionReason // ""') || fa="(hook が失敗した: rc=$?)"
if [[ "$fa" == *"awk"*"失敗"* ]]; then
  printf '✓ deny: awk が失敗したら判定を諦めて拒否に倒す\n'
else
  printf '✗ awk が失敗したのに拒否に倒さない: [%s]\n' "${fa:0:80}"
  fail=1
fi

# 数 MB の入力は、畳む・落とす前に粗い上限で止める。🚨 効くのは /bin/bash 3.2 だけ (畳む置換が入力長の 2 乗で遅く、上限が無いと 9MB で
# timeout を越えて無出力 = 素通り。bash 5 は速く、後ろの 128KB の上限が同じ deny を出す) ので、/bin/bash で走らせる。引数に載らないのでファイルで渡す
if [ -x /bin/bash ]; then
  checked=$((checked + 1))
  python3 -c "import json; print(json.dumps({'tool_input': {'command': 'git push | tail && git worktree remove x\\n' + 'y' * 9000000}}))" > "$fake/big.json"
  # 🚨 hook が timeout に殺されるとパイプが失敗し、set -e で検査ごと無言で止まる。rc を取って「止めていない」として報告する
  br=$("$TIMEOUT_BIN" "$HOOK_TIMEOUT" /bin/bash "$HOOK" < "$fake/big.json" | jq -r '.hookSpecificOutput.permissionDecisionReason // ""') || br="(hook が失敗した / timeout: rc=$?)"
  if [[ "$br" == *"大きすぎて"* ]]; then
    printf '✓ deny: 9MB の入力は /bin/bash でも粗い上限で止める (上限の時間内)\n'
  else
    printf '✗ 9MB の入力を /bin/bash で粗い上限で止めていない (timeout で素通り?): [%s]\n' "${br:0:80}"
    fail=1
  fi
else
  echo "✗ /bin/bash が無く、粗い上限の検査を走らせられない"
  fail=1
fi

[ "$checked" -ge 20 ] || { echo "✗ 検査の数が少ない ($checked)"; exit 1; }
echo "検査 $checked 件"
exit "$fail"
