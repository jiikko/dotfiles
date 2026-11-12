#!/usr/bin/env bash
# inventory.sh — Claude Code の資産 (settings の hooks・rules・skills・agents) を読み取りだけで数え、
#   公式の欄の一覧と、手元の claude のバイナリに機能名が在るかを並べる。claude-assets-review skill の Step 1 が使う。
#
# 使い方: inventory.sh <claude dir の絶対パス>
#   claude dir = settings.json・rules/・skills/・agents/ を持つ dir (dotfiles は _claude、repo は .claude、global は ~/.claude)
#
# 何も書かない (一時ファイルは mktemp で作り、終わりに消す)。jq が要る。
#
# 公式の欄の一覧 (KNOWN_*) と機能名 (FEATURES) は 2026-11-13 に公式ドキュメント (code.claude.com/docs/en/ の
#   skills / sub-agents / hooks) と claude 2.1.322 で採った値。claude のバージョンが上がったら SKILL.md の Step 0 で
#   ドキュメントを読み直し、ここを直す (一覧に無い欄は「非公式」と出るだけで、間違いとは限らない)。
#
# 検出しないもの: hook のスクリプトの中身 (何を止めるか)・rule の文面の重複・plugin (mods) の中の hook・
#   `if` の照合が複合コマンドで取りこぼすか。これらは SKILL.md の観点で人 (モデル) が読む。
set -uo pipefail

KNOWN_SKILL_KEYS="name description when_to_use argument-hint arguments disable-model-invocation user-invocable allowed-tools disallowed-tools model effort context agent background hooks paths shell metadata license compatibility"
KNOWN_AGENT_KEYS="name description tools disallowedTools model effort permissionMode maxTurns autoCompactWindow background color skills memory omitClaudeMd initialPrompt isolation hooks mcpServers experimental"
FEATURES="asyncRewake InstructionsLoaded PostCompact PreCompact SubagentStart omitClaudeMd when_to_use skillListingMaxDescChars claudeMdExcludes onFailure blockWritesOutsideWorkingDirectories"
LISTING_DESC_LIMIT=1536 # description + when_to_use がこれを超えると一覧で切り詰められる (skills ドキュメント)
SKILL_MD_LINES=500      # SKILL.md の推奨上限 (同)

die() { printf 'inventory.sh: %s\n' "$*" >&2; exit 2; }
command -v jq >/dev/null 2>&1 || die "jq が見つからない"
dir=${1:-}
[ -n "$dir" ] || die "使い方: inventory.sh <claude dir の絶対パス>"
case $dir in /*) ;; *) die "絶対パスで渡す: $dir" ;; esac
[ -d "$dir" ] || die "dir が無い: $dir"

frontmatter() { awk 'NR==1 && $0!="---"{exit} NR==1{next} $0=="---"{exit} {print}' "$1"; }
fm_keys() { frontmatter "$1" | grep -oE '^[A-Za-z_-]+:' | tr -d ':'; }
in_list() { case " $2 " in *" $1 "*) return 0 ;; esac; return 1; }

seen=0
echo "# claude: $(claude --version 2>/dev/null || echo '見つからない')"

echo; echo "## rules ($dir/rules)"
if [ -d "$dir/rules" ]; then
  n=0; np=0
  while IFS= read -r f; do
    n=$((n + 1))
    if grep -q '^paths:' <<<"$(frontmatter "$f")"; then np=$((np + 1)); fi
  done < <(find "$dir/rules" -name '*.md' -type f)
  seen=$((seen + n))
  echo "本数 $n / paths: で条件ロード $np / 常時読み込み $((n - np))"
  echo "大きい順 (字数):"
  find "$dir/rules" -name '*.md' -type f -exec sh -c 'LC_ALL=en_US.UTF-8 wc -m "$1"' _ {} \; | sort -rn | head -5 | sed 's/^/  /'
else echo "無い"; fi

echo; echo "## hooks ($dir/settings.json)"
if [ -f "$dir/settings.json" ]; then
  seen=$((seen + 1))
  jq -r '(.hooks // {}) | to_entries[] |
    "\(.key): 組 \(.value|length) / handler \([.value[].hooks[]?]|length) / 型 \([.value[].hooks[]?.type]|unique|join(",")) / matcher なしの組 \([.value[]|select((.matcher // "")=="" or .matcher=="*")]|length) / if あり \([.value[].hooks[]?|select(.if)]|length) / async \([.value[].hooks[]?|select(.async)]|length)"' "$dir/settings.json"
  echo "matcher ごとの handler 数 (PreToolUse・PostToolUse):"
  jq -r '(.hooks // {}) | to_entries[] | select(.key=="PreToolUse" or .key=="PostToolUse") | .key as $e | .value[] |
    "  \($e) \(.matcher // "*"): \(.hooks|length) 本 (if なし \([.hooks[]|select(.if|not)]|length))"' "$dir/settings.json"
else echo "settings.json が無い"; fi

echo; echo "## agents ($dir/agents)"
if [ -d "$dir/agents" ]; then
  unrestricted=""; unknown=""; n=0
  for f in "$dir"/agents/*.md; do
    [ -f "$f" ] || continue
    case $(basename "$f") in README.md|CLAUDE.md) continue ;; esac
    n=$((n + 1)); name=$(basename "$f" .md)
    grep -qE '^(tools|disallowedTools)$' <<<"$(fm_keys "$f")" || unrestricted="$unrestricted $name"
    for k in $(fm_keys "$f"); do in_list "$k" "$KNOWN_AGENT_KEYS" || unknown="$unknown $name:$k"; done
  done
  seen=$((seen + n))
  echo "本数 $n"
  echo "tools / disallowedTools の無い agent (全部の道具を持つ):${unrestricted:- なし}"
  echo "公式の一覧に無い欄:${unknown:- なし}"
else echo "無い"; fi

echo; echo "## skills ($dir/skills)"
if [ -d "$dir/skills" ]; then
  n=0; long_desc=""; long_md=""; tmpk=$(mktemp); tmpu=$(mktemp); trap 'rm -f "$tmpk" "$tmpu" "${tmps:-}"' EXIT
  for f in "$dir"/skills/*/SKILL.md; do
    [ -f "$f" ] || continue
    n=$((n + 1)); name=$(basename "$(dirname "$f")")
    fm_keys "$f" >>"$tmpk"
    for k in $(fm_keys "$f"); do in_list "$k" "$KNOWN_SKILL_KEYS" || printf '%s %s\n' "$k" "$name" >>"$tmpu"; done
    d=$(frontmatter "$f" | grep -E '^(description|when_to_use):' | sed -E 's/^[a-z_]+: *//' | tr -d '\n')
    [ ${#d} -gt $LISTING_DESC_LIMIT ] && long_desc="$long_desc $name(${#d})"
    l=$(wc -l <"$f" | tr -d ' ')
    [ "$l" -gt $SKILL_MD_LINES ] && long_md="$long_md $name($l)"
  done
  seen=$((seen + n))
  echo "本数 $n"
  echo "frontmatter の欄 (使っている skill の数):"
  sort "$tmpk" | uniq -c | sort -rn | sed 's/^/  /'
  echo "公式の一覧に無い欄 (欄: 本数 skill):"
  if [ -s "$tmpu" ]; then
    awk '{c[$1]++; l[$1]=l[$1] " " $2} END{for (k in c) printf "  %s: %d 本%s\n", k, c[k], (c[k] > 3 ? " (" substr(l[k], 2, 60) "...)" : l[k])}' "$tmpu" | sort
  else echo "  なし"; fi
  echo "description が一覧の上限 $LISTING_DESC_LIMIT 字を超える:${long_desc:- なし}"
  echo "SKILL.md が $SKILL_MD_LINES 行を超える:${long_md:- なし}"
else echo "無い"; fi

echo; echo "## 手元の claude のバイナリに機能名が在るか (strings で数えた件数。0 はこのバージョンに無い見込み)"
bin=$(command -v claude 2>/dev/null) && bin=$(readlink -f "$bin")
if [ -n "${bin:-}" ] && [ -f "$bin" ]; then
  tmps=$(mktemp); strings "$bin" >"$tmps" 2>/dev/null
  # 較正: 必ず在る語で strings が空振りしていないかを先に見る
  grep -q 'PreToolUse' "$tmps" || die "strings の出力に PreToolUse が無い (読み方が空振りしている): $bin"
  for s in $FEATURES; do printf '  %-40s %s\n' "$s" "$(grep -c -- "$s" "$tmps")"; done
else echo "  claude のバイナリが見つからない (判定不能)"; fi

[ "$seen" -gt 0 ] || die "rules・settings・agents・skills のどれも見つからない (dir の取り違え?): $dir"
