#!/usr/bin/env bash
#
# SessionStart hook 共通処理。issues/ を読んでコンテキストへ注入する hook
# (human-tasks-due.sh / retro-open.sh) が source する。issue_hook_emit だけは issue と無関係な
# claude-links-sync.sh も使う (additionalContext の JSON 形を 1 箇所に保つため)。
#
# なぜ共通化するか: 「stdin の hook JSON から cwd を取る / repo root を出す / issues か issue か
# を判定する / jq が無い環境でも黙らない」は全 issue 系 hook で同一で、コピーすると片方だけ
# 直した壊れ方 (= 静かに黙る hook が残る) を作る。ここが唯一の出典。
#
# 使い方:
#   . "$(dirname "$0")/lib/issue-hooks.sh"
#   issue_hook_resolve_dir || exit 0          # ISSUE_HOOK_ROOT / ISSUE_HOOK_DIR を設定
#   issue_hook_emit "<指示文>" "<報告本文>"    # 報告があるときだけ呼ぶ

# issue_hook_resolve_dir: stdin の hook JSON (`.cwd`) か $PWD から repo root と issues
# ディレクトリを解決する。見つからなければ非 0 を返す (呼び出し側は黙って exit 0 する)。
issue_hook_resolve_dir() {
  local input="" cwd="$PWD" from_json root cand
  [ -t 0 ] || input=$(cat 2>/dev/null || true)
  if [ -n "$input" ]; then
    if command -v jq >/dev/null 2>&1; then
      from_json=$(printf '%s' "$input" | jq -r '.cwd // empty' 2>/dev/null || true)
    else
      # 🚨 jq が無いときに `.cwd` を捨てて $PWD へ落とすと、hook が「別 repo の issues を
      # 報告する」= 黙って間違う (実測 2026-08-21)。沈黙より誤報の方が悪いので sed で拾う
      from_json=$(printf '%s' "$input" | tr -d '\n' \
        | sed -n 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
    fi
    [ -n "$from_json" ] && [ -d "$from_json" ] && cwd="$from_json"
  fi
  root=$(git -C "$cwd" rev-parse --show-toplevel 2>/dev/null || true)
  [ -n "$root" ] || return 1
  # issues/ と issue/ (単数) の両方、および**入れ子の 1 段** (`<root>/*/issues`) を受ける。
  #
  # 🚨 root 直下だけを見ないこと (issue 276)。obaket は `macOS/issues/` を正式に持っており
  # (apps/obaket/.claude/rules/issue-placement.md)、そこに置かれた human の期限切れ /
  # retro の未決着 / next/ の claim が **どの hook にも見えなかった**。
  # 深さは 1 段に限る: 全走査にすると node_modules / .git / ビルド生成物まで舐める。
  local nl=$'\n' # 🚨 $'\n' は**二重引用符の外**でないと ANSI-C 展開されない (リテラルになる)
  ISSUE_HOOK_DIRS=""
  for cand in "$root/issues" "$root/issue" "$root"/*/issues "$root"/*/issue; do
    [ -d "$cand" ] || continue
    ISSUE_HOOK_DIRS="${ISSUE_HOOK_DIRS}${ISSUE_HOOK_DIRS:+$nl}$cand"
  done
  [ -n "$ISSUE_HOOK_DIRS" ] || return 1
  # 呼び出し側 (source した hook) が読む出力変数
  # shellcheck disable=SC2034
  ISSUE_HOOK_ROOT="$root"
  # ISSUE_HOOK_DIR は「最初の 1 つ」の後方互換。**新しい呼び出しは ISSUE_HOOK_DIRS を
  # 使うこと** (root 直下しか無い repo では両者は同じ)。
  # shellcheck disable=SC2034
  ISSUE_HOOK_DIR=${ISSUE_HOOK_DIRS%%"$nl"*}
  return 0
}

# issue_hook_category <basename>: `NNN-<カテゴリ>-<スラッグ>.md` のカテゴリを stdout に出す。
# 番号 (数字のみ。桁数は問わない = 1000 番以降も捨てない) で始まらないものは非 0 を返す。
#
# 🚨 `*-human-*` / `*-retro-*` のような部分一致でカテゴリを判定しない。スラッグ側に同じ語を
# 含む別カテゴリを誤検出する (実測 2026-08-20: 061-docs-mutation-verify-fake-mutations.md を
# human として拾った)。逆に `[0-9][0-9][0-9]*-retro-*` のように `*` を挟むと今度は
# `011-docs-retro-format-notes.md` を飲む (実測 2026-08-21)。position 2 を切り出すのが唯一の解。
issue_hook_category() {
  local base="$1" num rest
  num=${base%%-*}
  case "$num" in
    '' | *[!0-9]*) return 1 ;;
  esac
  rest=${base#*-}
  [ "$rest" != "$base" ] || return 1
  rest=${rest%.md}
  printf '%s' "${rest%%-*}"
}

# issue_hook_emit <指示文> <報告本文>: additionalContext として出す。
# jq が無い環境でも黙らない (SessionStart の stdout はそのままコンテキストに入る)。
issue_hook_emit() {
  local ctx="$1"$'\n'"$2"
  if command -v jq >/dev/null 2>&1; then
    jq -n --arg ctx "$ctx" '{
      hookSpecificOutput: {
        hookEventName: "SessionStart",
        additionalContext: $ctx
      },
      suppressOutput: true
    }'
  else
    printf '%s' "$ctx"
  fi
}
