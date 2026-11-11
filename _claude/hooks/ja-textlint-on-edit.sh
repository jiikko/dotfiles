#!/usr/bin/env bash
# PostToolUse(Write / Edit) フック: Markdown を書いた直後に、そのファイルを bin/ja-textlint に通し、
# 書いた行に出た自作の規則の指摘 (ja-ai-vocabulary: AI が書きがちな語 / ja-bold-not-rendered: 表示されない太字) を知らせる。
#
# なぜ: 書いた後に ja-textlint を手で打つのは忘れる。commit の後に見つかると、直すために同じファイルを開き直す。
#
# 🚨 **止めない (additionalContext だけ)**。指摘が無いときは何も出さない。
# 🚨 **知らせるのは書いた行だけ**。Write はファイル全体、Edit は new_string の行のうち old_string に同じ行が無いもの
#   (new_string には書き換えていない前後の文脈の行も入るので、それを除く)。new_string がファイルに複数あれば全部の出現を見る。
#   前からある指摘まで毎回出すと、同じファイルを触るたびに同じ知らせが積もって読まれなくなる。
# 🚨 **160KB を超えるファイルは見ない**。textlint はファイル全体を読むので、大きいと hook の上限 (15 秒) を超えて編集のたびに待たされる
#   (repo で最大の 148KB の issue は 5 秒、合成した 200KB は 66 秒)。
# 🚨 **cwd の .textlintignore を読まない** (--ignore-path /dev/null)。session の cwd の ignore で黙って知らせが消える。
# 🚨 **preset の指摘 (文の長さ・助詞の重複など) は知らせない**。dotfiles の文書には何百件もあり、毎回の知らせが埋もれる。
#   全部を見たいときは ja-textlint を手で打つ (skill ja-writing)。
# 🚨 **依存が入っていなければ入れない** (JA_TEXTLINT_NO_INSTALL)。編集のたびに npm を走らせない。一度手で ja-textlint を打てば入る。
# どの repo のファイルでもかける。走らせるのはこの hook と同じ repo の bin/ja-textlint だけで、書いた先の repo のコードは実行しない。
#
# 検出しない形 (silent): Bash の書き込み (sed / heredoc)・NotebookEdit・Markdown 以外の拡張子・jq が無い環境・依存が未導入・
#   ja-textlint が指摘の有無以外で終わったとき (fail-open。規則の誤りは tests/bin/test_ja_textlint.sh が見る)・160KB を超えるファイル・
#   Edit で new_string が空 (削除だけ) のとき・new_string がファイルに見つからないとき (書いた後に別の hook が書き換えた等)・
#   old_string にあった行を同じ文字のまま別の場所へ動かしただけの行
# 分かっていて受ける形: replace_all でない Edit で new_string がファイルの別の場所にもあると、書いていない方の行も見る
#   (どの出現を書いたかは、書いた後のファイルからは決まらない。old_string と同じ行は除くので、文脈の行は出ない)

set -uo pipefail
unset CDPATH

command -v jq >/dev/null 2>&1 || exit 0

self=$(readlink -f "$0" 2>/dev/null) || exit 0
hook_repo=$(cd "$(dirname "$self")/../.." && pwd -P) || exit 0
lint="$hook_repo/bin/ja-textlint"
[ -x "$lint" ] || exit 0

input=$(cat)
file=$(printf '%s' "$input" | jq -r '.tool_input.file_path // ""' 2>/dev/null) || exit 0
case "$file" in
  /*) ;;
  *) exit 0 ;;
esac
# 拡張子は textlint の markdown plugin が受けるものに揃える (それ以外は text plugin になり、太字の規則が走らない)
case "$file" in
  *.md | *.markdown | *.mdown | *.mkdn | *.mkd | *.mdwn | *.mkdown) ;;
  *) exit 0 ;;
esac
[ -f "$file" ] || exit 0
size=$(wc -c < "$file" 2>/dev/null) || exit 0
[ "$size" -le 160000 ] || exit 0

# 書いた行の範囲を [開始, 終了] の組で出す (行は 1 始まり)。jq の indices と文字列の切り出しはどちらもコードポイント単位。
# 行は「前にある改行の数 + 1」で数える。split の要素数で数えない: 空の文字列の split は 0 要素で、ファイルの先頭が 0 行目になる。
# CR は位置を探す前に外す (CRLF のファイルでも new_string は LF で届く)。行の数は CR を外しても変わらない
ranges=$(printf '%s' "$input" | jq -c --rawfile c "$file" '
  ($c | gsub("\r"; "")) as $c
  | if .tool_name == "Edit" then
      (.tool_input.new_string // "" | gsub("\r"; "")) as $s
      | (.tool_input.old_string // "" | gsub("\r"; "") | split("\n")) as $old
      | ($s | split("\n")) as $nl
      | [ range(0; $nl | length) | . as $k | select($nl[$k] != "" and (any($old[]; . == $nl[$k]) | not)) ] as $ks
      | if $s == "" or ($ks | length) == 0 then [] else
          [ $c | indices($s)[] | ([$c[:.] | match("\n"; "g")] | length + 1) as $l | $ks[] | [$l + ., $l + .] ]
        end
    else [[1, 1e9]] end' 2>/dev/null) || exit 0
[ "$ranges" != "[]" ] || exit 0

out=$(JA_TEXTLINT_NO_INSTALL=1 "$lint" --ignore-path /dev/null -f json "$file" 2>/dev/null)
rc=$?
[ "$rc" -le 1 ] || exit 0

notice=$(printf '%s' "$out" | jq -r --argjson r "$ranges" '
  [ .[].messages[]
    | select(.ruleId == "ja-ai-vocabulary" or .ruleId == "ja-bold-not-rendered")
    | . as $m | select(any($r[]; $m.line >= .[0] and $m.line <= .[1]))
    | "\(.line):\(.column) \(.message)" ] as $all
  | if ($all | length) == 0 then empty
    else ($all[:20] | join("\n")) + (if ($all | length) > 20 then "\n… ほか \(($all | length) - 20) 件" else "" end)
    end' 2>/dev/null) || exit 0
[ -n "$notice" ] || exit 0

msg="ja-textlint: ${file} の書いた行に、AI が書きがちな語・表示されない太字がある
$notice
起きていることの言葉に直す (skill ja-writing の規範 7)。言い換えると意味が変わる所 (定義した用語・引用・画面の文言) は元の語を残す"
jq -n --arg c "$msg" '{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $c}}'
