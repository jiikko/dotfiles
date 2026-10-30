#!/usr/bin/env bash
# frontmatter に paths: を持つ rule が、どの操作で読み込まれるかを今の Claude Code の版で測る (claude-md-compact の Step 3)。
#   使い捨てのプロジェクトに paths: "probe/**" の rule (合言葉入り) を置き、headless のセッション (haiku・hook なし) に
#   操作をさせてから合言葉を答えさせる。none と read が較正 (none = NONE / read = 合言葉) で、これが外れたら他の行を読まない。
# 使い方: paths-probe.sh [none read write edit ...]   (省略時は 4 通り。1 通りごとにモデルを 1 回呼ぶ)
# 出力: 1 行 1 通り (操作<TAB>rc<TAB>使った道具<TAB>答え)。生の stream-json と版は skill-tmpdir の dir に残す
set -uo pipefail
unset CDPATH

out="$(skill-tmpdir claude-md-compact 2>/dev/null || mktemp -d "${TMPDIR:-/tmp}/paths-probe.XXXXXX")/paths-probe-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$out"
claude --version > "$out/version.txt" 2>&1
printf '版: %s / 出力: %s\n' "$(cat "$out/version.txt")" "$out"

[ "$#" -gt 0 ] || set -- none read write edit
for case_name in "$@"; do
  case "$case_name" in
    read)  task='probe/a.txt を Read ツールで読んでください。' ;;
    write) task='Write ツールで probe/b.txt を作り、中身を "x" にしてください (先に何も Read しないこと)。' ;;
    edit)  task='Edit ツールで probe/a.txt の hello を bye に変えてください。Edit の前に必要な Read はしてよいが、Bash は使わないこと。' ;;
    none)  task='ファイルには何も触らないでください。' ;;
    *) printf '✗ 知らない操作: %s (none / read / write / edit)\n' "$case_name" >&2; exit 2 ;;
  esac
  proj="$(mktemp -d "${TMPDIR:-/tmp}/paths-probe-proj.XXXXXX")"
  mkdir -p "$proj/.claude/rules" "$proj/probe"
  git -C "$proj" init -q
  printf -- '---\npaths:\n  - "probe/**"\n---\n\n# probe\n\n合言葉は「みかんの木星」。この rule が読み込まれていれば、聞かれたときに合言葉を答える。\n' > "$proj/.claude/rules/probe.md"
  printf 'hello\n' > "$proj/probe/a.txt"
  prompt="$task そのあと、あなたの指示の中に「合言葉」が書かれていれば、その合言葉だけを 1 行で答えてください。無ければ NONE とだけ答えてください。"
  (cd "$proj" && claude -p --model haiku --settings '{"disableAllHooks":true}' \
    --output-format stream-json --verbose --permission-mode acceptEdits \
    "$prompt" --allowedTools "Read,Write,Edit" </dev/null) > "$out/$case_name.jsonl" 2> "$out/$case_name.err"
  rc=$?
  tools=$(jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use") | .name' "$out/$case_name.jsonl" | paste -sd, -)
  answer=$(jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="text") | .text' "$out/$case_name.jsonl" | tail -1)
  printf '%s\t%s\t%s\t%s\n' "$case_name" "$rc" "${tools:--}" "${answer:-(空)}" | tee -a "$out/summary.tsv"
  rm -rf "$proj"
done
