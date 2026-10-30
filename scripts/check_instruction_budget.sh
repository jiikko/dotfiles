#!/usr/bin/env bash
# check_instruction_budget.sh — 毎セッション全文読まれる指示ファイルの合計を、Claude Code の上限 150k 字の内に保つ。
#   Claude Code は起動時に、常時読み込む指示ファイル (CLAUDE.md と frontmatter に `paths:` の無い rule) の合計が 150k 字を超えると
#   `⚠ N instruction files add up to …k chars, over the 150.0k-char total limit` を出す。
#
# 数えるもの (`LC_ALL=en_US.UTF-8 wc -m`。警告の値とほぼ一致した数え方):
#   - global 層: `_claude/CLAUDE.md` + `_claude/rules/` の下の `*.md` (どの repo のセッションにも載る)
#   - dotfiles の repo 層: `CLAUDE.md` + `.claude/rules/` の下の `*.md`
#   rules はサブディレクトリも数える (scripts/claude_links.sh はディレクトリごと link する。読まれないなら多く数えて赤に倒れるだけ)
#   frontmatter (1 行目の `---` から次の `---` まで) に `paths:` の行がある rule は条件ロードなので数えない。
#
# 落とす条件:
#   - global 層が LIMIT - OTHER_REPO_RESERVE を超えた
#   - global 層 + dotfiles の repo 層が LIMIT を超えた
#   - `_claude/CLAUDE.md` が無い・数えた rule が 0 本 (数え方が空振りした)
#
# OTHER_REPO_RESERVE は、global 層に repo 層を足すセッションのうち最も重いものの repo 層。CI からは他の repo を読めないので定数で持つ。
#   測り方: gh api で各 repo の CLAUDE.md と .claude/rules を取り、paths: の無いものを wc -m で数える (clone は要らない)。
#   2026-10-30: my-products 14,089 + obaket 19,573 + obaket/macOS 5,011 = 38,673 (obaket の macOS/ で起動したセッション)。
#   他の repo で上限超えの警告が出たら測り直して上げる。下げるのも測り直したときだけ。
#
# 検出しないもの:
#   - 他の repo の repo 層の増加 (上の定数が古くなる)・auto memory (MEMORY.md)・hook が注入する文 (system-reminder)
#   - Claude Code の数え方との差 (UTF-16 で数えるなら 🚨 などの絵文字は 2 字。差は数十字の規模)
#   - git に add していないファイルも数える (作業ツリーを見る)
#   - CLAUDE.md の `@path` の import が展開する中身 (今は 0 件)
#   - `paths:` の値 (空・YAML として壊れている) は見ない。キーがあれば条件ロードと見なす
set -uo pipefail
unset CDPATH

LIMIT=150000
OTHER_REPO_RESERVE=39000
root="${INSTRUCTION_BUDGET_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"

# rc=0: frontmatter に paths: がある (条件ロード)
has_paths_frontmatter() {
  awk 'NR==1 { if ($0 != "---") { r = 1; exit } next }
       $0 == "---" { r = f ? 0 : 1; done = 1; exit }
       /^paths:/ { f = 1 }
       END { if (!done) r = 1; exit r }' "$1"
}

chars() { LC_ALL=en_US.UTF-8 wc -m < "$1" | tr -d ' '; }

sizes=""  # "<字数> <path>" の行。上限超えのときに大きい順に出す
layer_total=0
layer_files=0
count_layer() {  # $@ = 候補のファイル。結果は layer_total / layer_files
  layer_total=0
  layer_files=0
  local f n
  for f in "$@"; do
    [ -f "$f" ] || continue
    has_paths_frontmatter "$f" && continue
    n=$(chars "$f")
    layer_total=$((layer_total + n))
    layer_files=$((layer_files + 1))
    sizes="$sizes$n ${f#"$root"/}"$'\n'
  done
}

if [ ! -f "$root/_claude/CLAUDE.md" ]; then
  printf '✗ %s/_claude/CLAUDE.md が無い (数える対象を取り違えている)\n' "$root" >&2
  exit 1
fi
rule_files() { [ -d "$1" ] && find "$1" -name '*.md' \( -type f -o -type l \) | LC_ALL=C sort; }
# shellcheck disable=SC2046  # パスに空白を含まない (rule の名前は kebab-case)
count_layer $(rule_files "$root/_claude/rules")
rules_files=$layer_files
if [ "$rules_files" -eq 0 ]; then
  printf '✗ %s/_claude/rules/ に常時読み込みの rule が 1 本も無い (数え方が空振りした)\n' "$root" >&2
  exit 1
fi
rules_total=$layer_total
count_layer "$root/_claude/CLAUDE.md"
global=$((rules_total + layer_total))
# shellcheck disable=SC2046  # 同上
count_layer "$root/CLAUDE.md" $(rule_files "$root/.claude/rules")
session=$((global + layer_total))
global_limit=$((LIMIT - OTHER_REPO_RESERVE))

printf '常時読み込み: global 層 %d 字 (_claude/CLAUDE.md + rule %d 本。上限 %d = %d - 他の repo 層の分 %d) / dotfiles のセッション %d 字 (上限 %d)\n' \
  "$global" "$rules_files" "$global_limit" "$LIMIT" "$OTHER_REPO_RESERVE" "$session" "$LIMIT"

rc=0
if [ "$global" -gt "$global_limit" ]; then
  printf '✗ global 層が %d 字超えている。obaket などのセッションで Claude Code の上限 (150k 字) を超える\n' "$((global - global_limit))" >&2
  rc=1
fi
if [ "$session" -gt "$LIMIT" ]; then
  printf '✗ dotfiles のセッションが上限を %d 字超えている\n' "$((session - LIMIT))" >&2
  rc=1
fi
if [ "$rc" -ne 0 ]; then
  printf '  大きい順:\n' >&2
  printf '%s' "$sizes" | sort -rn | head -8 | sed 's/^/    /' >&2
  printf '  縮め方は issue 414: 手順の詳細を _claude/rules-checklists/、なぜ・実例を _claude/rules-rationale/ へ移す。ルール節の言い換えの節を消す\n' >&2
fi
exit "$rc"
