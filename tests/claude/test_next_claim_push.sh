#!/usr/bin/env bash
# _claude/hooks/next-claim-push.sh (PostToolUse(Bash): issues/next/ または
# issues/epic/<name>/next/ への claim を検出して
# 「単独 commit して push したか」を注入する) の unit テスト。
#
# なぜ: この hook は「別マシンと同じ issue を二重に着手する」事故 (2026-09-02 に実際に発生) を
# harness 側で減らす装置。判定式が壊れると **無言で発火しなくなる** = 防御がゼロに戻る。
# 規範: _claude/rules/claim-issue-in-next-and-push.md
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/next-claim-push.sh"
HOOK_TIMEOUT=10

# 本番と同じ上限を課せないなら「合格」ではなく「判定不能 = 失敗」にする
# (tests/claude/test_deny_bare_tmux_kill.sh と同じ規律。CI で timeout(1) が消えて
#  検査が丸ごと skip に化けた事故がある)。
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_BIN=timeout
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_BIN=gtimeout
else
  echo "✗ timeout(1) / gtimeout(1) がどちらも無い。本番と同じ上限を課せないので検査できない" >&2
  exit 1
fi

command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }

fail=0
ok=0

# 判定式の検査は **global または group 内の next/ が実在する偽 repo** の中で走らせる。
# 🚨 本物の dotfiles で走らせてはいけない: `issues/next/` / group 内 `next/` は空のときディレクトリごと
# git に載らないので、**新品チェックアウトと CI には存在しない**。hook は
# 「global または group 内の next/ が実在する repo でだけ有効」(opt-in) なので、前提を作らずに呼ぶと
# 判定式が壊れていなくても全件が無出力になり、9 件が落ちる (実測 2026-09-02: CI run
# 33649890092。手元では過去の作業で残った issues/next/ があったため気づけなかった)。
# 偽 repo にしておけば本物の repo を汚さず、opt-in の有無も下の scope テストで別に見られる。
FIRE_REPO=$(mktemp -d)
EPIC_FIRE_REPO=$(mktemp -d)
NESTED_FIRE_REPO=$(mktemp -d)
cleanup() { rm -rf "$FIRE_REPO" "$EPIC_FIRE_REPO" "$NESTED_FIRE_REPO" "${scope_tmp:-}"; }
trap cleanup EXIT
(
  cd "$FIRE_REPO" && git init -q . && mkdir -p issues/next && : > issues/186-x.md &&
    git add -A && git -c user.email=t@t -c user.name=t commit -qm init
) >/dev/null 2>&1

# global next/ が無くても group 内 next/ だけで opt-in されることを検査する。
(
  cd "$EPIC_FIRE_REPO" && git init -q . && mkdir -p issues/epic/foo/next &&
    : > issues/epic/foo/186-x.md &&
    git add -A && git -c user.email=t@t -c user.name=t commit -qm init
) >/dev/null 2>&1

# 入れ子の issue dir (`<root>/*/issues/next/`) だけで opt-in されることを検査する (issue 276)。
# 🚨 276 は「4 hook すべてに入れ子のケースを足した」と書いたが、**この hook だけテストが
# 1 件も入っていなかった** (敵対的レビュー 2026-09-06 が変異で確認: 入れ子 glob 2 本を
# 削っても 29/29 緑のままだった)。
(
  cd "$NESTED_FIRE_REPO" && git init -q . && mkdir -p macOS/issues/next &&
    : > macOS/issues/186-x.md &&
    git add -A && git -c user.email=t@t -c user.name=t commit -qm init
) >/dev/null 2>&1

run_nested_hook() { # $1 = command 文字列 → 入れ子 repo で stdout に hook の出力
  (
    cd "$NESTED_FIRE_REPO" &&
      jq -n --arg c "$1" '{tool_input: {command: $c}}' |
      "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null
  ) || true
}

run_hook() { # $1 = command 文字列 → stdout に hook の出力
  (
    cd "$FIRE_REPO" &&
      jq -n --arg c "$1" '{tool_input: {command: $c}}' |
      "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null
  ) || true
}

run_epic_hook() { # $1 = command 文字列 → epic-only repo で stdout に hook の出力
  (
    cd "$EPIC_FIRE_REPO" &&
      jq -n --arg c "$1" '{tool_input: {command: $c}}' |
      "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null
  ) || true
}

expect_fire() {
  local desc=$1 cmd=$2 out
  out=$(run_hook "$cmd")
  if [ -z "$out" ]; then
    echo "✗ 発火すべきなのに無出力: $desc — $cmd"; fail=$((fail + 1)); return
  fi
  # 出力が JSON として妥当で、規範へのポインタを含むこと (壊れた JSON は素通りと同じ)
  if ! printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | test("claim-issue-in-next-and-push")' >/dev/null 2>&1; then
    echo "✗ JSON が壊れている / 本文が期待と違う: $desc"; fail=$((fail + 1)); return
  fi
  ok=$((ok + 1))
}

expect_epic_fire() {
  local desc=$1 cmd=$2 out
  out=$(run_epic_hook "$cmd")
  if [ -z "$out" ]; then
    echo "✗ epic-only repo で発火すべきなのに無出力: $desc — $cmd"; fail=$((fail + 1)); return
  fi
  if ! printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | test("claim-issue-in-next-and-push")' >/dev/null 2>&1; then
    echo "✗ JSON が壊れている / 本文が期待と違う: $desc"; fail=$((fail + 1)); return
  fi
  ok=$((ok + 1))
}

expect_silent() {
  local desc=$1 cmd=$2 out
  out=$(run_hook "$cmd")
  if [ -n "$out" ]; then
    echo "✗ 誤発火: $desc — $cmd"; fail=$((fail + 1)); return
  fi
  ok=$((ok + 1))
}

# --- 発火すべき形 ---
expect_fire "ln -s で目印 (現行の claim)"   "ln -s ../186-x.md issues/next/186-x.md"
expect_fire "ln -s で宛先がディレクトリ"     "ln -s ../186-x.md issues/next/"
expect_fire "ln -s の後に git add"           "ln -s ../186-x.md issues/next/186-x.md && git add issues/next/186-x.md"
expect_fire "git mv で next へ (旧運用)"     "git mv issues/186-x.md issues/next/"
expect_fire "素の mv で next へ"           "mv issues/186-x.md issues/next/"
expect_fire "末尾スラッシュ無し"           "git mv issues/186-x.md issues/next"
expect_fire "後続コマンドが続く形"         "git mv issues/186-x.md issues/next/ && echo done"
expect_fire "空白入りのパス"               'git mv "issues/186 x.md" issues/next/'

# --- 敵対的レビュー 2026-09-02 が P1 として見つけた抜け (当時は 4 件とも無検出だった)。
#     判定を行 grep から「区切りで割って移動先を見る」形へ作り替えたので、ここで固定する。
expect_fire "行継続で宛先が次の行"         "$(printf 'git mv issues/186-x.md \\\n  issues/next/')"
expect_fire "; が空白なしで隣接"           "git mv issues/186-x.md issues/next/; git add issues/next/186-x.md"
expect_fire "宛先にファイル名まで書く"     "git mv issues/186-x.md issues/next/186-x.md"
expect_fire "for ループの中"               'for f in issues/18*.md; do git mv "$f" issues/next/; done'

# --- group 内 next/ も検出し、global next/ が無い repo でも opt-in される ---
expect_epic_fire "epic group の next へ (ln -s)" "ln -s ../186-x.md issues/epic/foo/next/186-x.md"
expect_epic_fire "epic group の next へ" "git mv issues/epic/foo/186-x.md issues/epic/foo/next/"
expect_epic_fire "epic group の next へファイル名指定" \
  "git mv issues/epic/foo/186-x.md issues/epic/foo/next/186-x.md"

# --- 発火してはいけない形 ---
expect_silent "next から出す (claim 解除)" "git mv issues/next/186-x.md issues/"
expect_silent "目印を外す (rm)"             "git rm issues/next/186-x.md"
expect_silent "無関係な ln"                 "ln -s ../bin/foo scripts/foo"
expect_silent "next を見るだけ"            "ls issues/next/"
expect_silent "done への移動"              "git mv issues/186-x.md issues/done/"
expect_silent "無関係な commit"            "git commit -m x"
expect_silent "next を含む文字列だけ"      "grep -rn next issues/README.md"
expect_silent "next 配下から done へ"      "git mv issues/next/186-x.md issues/done/"

# --- 適用範囲: global / group 内の next/ が無い repo では丸ごと無効 (ユーザー要求 2026-09-02。
#     仕事の repo は issues/ を持たないので、そこでこの規律を出さない) ---
scope_tmp=$(mktemp -d)
(
  cd "$scope_tmp" && git init -q . && mkdir -p issues && : > issues/1.md &&
    git add -A && git -c user.email=t@t -c user.name=t commit -qm init
) >/dev/null 2>&1

# issues/ はあるが next/ が無い → 無効
out=$(cd "$scope_tmp" && jq -n --arg c "git mv issues/1.md issues/next/" '{tool_input: {command: $c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if [ -z "$out" ]; then ok=$((ok + 1)); else
  echo "✗ issues/next/ が無い repo で発火した (仕事の repo でも出てしまう)"; fail=$((fail + 1)); fi

# next/ を作れば有効 (opt-in が効くこと。無効側だけ見ると「常に黙る」退行を見逃す)
mkdir -p "$scope_tmp/issues/next"
out=$(cd "$scope_tmp" && jq -n --arg c "git mv issues/1.md issues/next/" '{tool_input: {command: $c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if [ -n "$out" ]; then ok=$((ok + 1)); else
  echo "✗ issues/next/ を作っても発火しない (opt-in が効いていない)"; fail=$((fail + 1)); fi

# git 管理外 → 無効
out=$(cd "$(mktemp -d)" && jq -n --arg c "git mv issues/1.md issues/next/" '{tool_input: {command: $c}}' |
  "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)
if [ -z "$out" ]; then ok=$((ok + 1)); else
  echo "✗ git 管理外で発火した"; fail=$((fail + 1)); fi

# --- 異常系: 壊れた入力で JSON を壊さない / 落ちない ---
if printf '' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" >/dev/null 2>&1; then
  ok=$((ok + 1))
else
  echo "✗ 空入力で異常終了した (PostToolUse は全 Bash 呼び出しで no-op であるべき)"; fail=$((fail + 1))
fi
if printf 'not json' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" >/dev/null 2>&1; then
  ok=$((ok + 1))
else
  echo "✗ 非 JSON 入力で異常終了した"; fail=$((fail + 1))
fi

# --- 入れ子の issue dir (issue 276) ---
if [ -n "$(run_nested_hook 'ln -s ../186-x.md macOS/issues/next/186-x.md')" ]; then
  ok=$((ok + 1))
else
  echo "✗ 入れ子 dir (macOS/issues/next/) への ln -s を検出しない (issue 276)"; fail=$((fail + 1))
fi
if [ -n "$(run_nested_hook 'git mv macOS/issues/186-x.md macOS/issues/next/')" ]; then
  ok=$((ok + 1))
else
  echo "✗ 入れ子 dir への git mv を検出しない (issue 276)"; fail=$((fail + 1))
fi

# --- 配線: hook 本体が正しくても settings.json から消えれば防御はゼロ ---
if jq -e '[.hooks.PostToolUse[] | select(.matcher == "Bash") | .hooks[].command] | any(test("next-claim-push"))' \
     "$ROOT_DIR/_claude/settings.json" >/dev/null 2>&1; then
  ok=$((ok + 1))
else
  echo "✗ _claude/settings.json の PostToolUse(Bash) に next-claim-push.sh が配線されていない"
  fail=$((fail + 1))
fi

echo "検査 $((ok + fail)) 件: ok=$ok / fail=$fail"
[ "$fail" -eq 0 ] || exit 1
