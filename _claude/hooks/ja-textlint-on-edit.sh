#!/usr/bin/env bash
# PostToolUse(Write / Edit) フック: Markdown を書いた直後に、そのファイルを bin/ja-textlint に通し、
# 書いた行に出た自作の規則の指摘 (ja-ai-vocabulary: AI が書きがちな語 / ja-bold-not-rendered: 表示されない太字) を知らせる。
# 続けて同じファイルに ja-writing lint (src/ja-writing) を掛け、書いた行の指摘のうち jw_rules の規則だけを知らせる。
#
# なぜ: 書いた後に ja-textlint を手で打つのは忘れる。commit の後に見つかると、直すために同じファイルを開き直す。
#
# 🚨 **止めない (additionalContext だけ)**。指摘が無いときは何も出さない。
# 🚨 **知らせるのは書いた行だけ**。Write はファイル全体、Edit は new_string の行のうち old_string に同じ行が無いもの
#   (new_string には書き換えていない前後の文脈の行も入るので、それを除く)。new_string がファイルに複数あれば全部の出現を見る。
#   前からある指摘まで毎回出すと、同じファイルを触るたびに同じ知らせが積もって読まれなくなる。
# 🚨 **時間は上限で止める**: textlint は 10 秒、行の範囲を出す jq は 3 秒で、子孫ごと止めて黙る (bin/runtimeout)。
#   textlint の時間は大きさ・行数だけでは決まらず (段落の形と指摘の数で伸びる。3000 行・156KB の和文で 48 秒、英文 2500 行で 60 秒超)、
#   入力の形の上限では hook の上限 (15 秒) の内に収められない。
#   160KB か 3000 行を超えるファイルを見ないのは、毎回の編集で 10 秒待たせないための前段の絞り込み (2000 行の和文で 5 秒)。
# 🚨 **cwd の .textlintignore を読まない** (--ignore-path /dev/null)。session の cwd の ignore で黙って知らせが消える。
# 🚨 **preset の指摘 (文の長さ・助詞の重複など) は知らせない**。dotfiles の文書には何百件もあり、毎回の知らせが埋もれる。
#   全部を見たいときは ja-textlint を手で打つ (skill ja-writing)。
# 🚨 **依存が入っていなければ入れない** (JA_TEXTLINT_NO_INSTALL)。編集のたびに npm を走らせない。一度手で ja-textlint を打てば入る。
# 🚨 **ja-writing lint もビルドしない** (go_autobuild_exec --no-build)。ビルドが無いか古ければ黙る。一度 bin/ja-writing を打てば入る。
#   --async にしない: 古いバイナリの結果を今の結果と取り違える。時間は 1 回 1 秒で止める (3000 行で 0.4 秒。一覧の無い repo では 2 回起こす。
#   jq 3 + textlint 10 + 1 + 1 で hook の上限 15 秒に収める)。
#   行の番号は ja-writing lint の数え方で、U+2028・U+2029 を改行に数えない。それを含む行より後ろの ja-writing の知らせは、範囲 (textlint に合わせて数える) と
#   食い違って出ないことがある (分かっていて受ける。textlint の知らせを優先する)。
#   ja-textlint は -f json のとき ja-writing lint を掛けない (機械が読む出力に別の形を混ぜない) ので、ここで直接起こす。
# 🚨 **ja-writing lint で知らせる規則は jw_rules だけ** (2026-11-25 に dotfiles の文書 80 本の当たりを読んで決めた)。
#   外したもの: emoji_prohibited (🚨 は dotfiles の書き方)・trailing_colon (行末の「:」も同じ)・dash_decoration など装飾のダッシュ (ユーザー決定)・
#   metaphor_label (issue の「## 進捗」の見出しに当たる)・info の規則 (negative_parallelism・renyo_noun・katakana_term。読んで直す要否の分かれる候補)・
#   文書全体の比率の規則 (excess_list・excess_bold・bold_label_list。書いた行に結び付かない)。
#   語の一覧: 書いたファイルの repo が自分の .yomiyasu-words/ を持てばそれを使い、無ければ dotfiles の一覧 (_claude/skills/ja-writing/words/) を渡す
#   (bin/ja-textlint と同じ決まり。ja-writing lint を書いたファイルのフォルダで起こし、一覧を使ったかは --json の context.words で見る)。
# どの repo のファイルでもかける。走らせるのはこの hook と同じ repo の bin/ja-textlint だけで、書いた先の repo のコードは実行しない。
#
# 検出しない形 (silent): Bash の書き込み (sed / heredoc)・NotebookEdit・Markdown 以外の拡張子・jq が無い環境・依存が未導入・
#   ja-textlint が指摘の有無以外で終わったとき (fail-open。規則の誤りは tests/bin/test_ja_textlint.sh が見る)・160KB か 3000 行を超えるファイル・
#   ja-writing lint のビルドが無い・古い・時間の上限で止められた・指摘の有無以外で終わったとき (textlint の知らせだけを出す)・
#   Edit で new_string が空 (削除だけ) のとき・new_string がファイルに見つからないとき (書いた後に別の hook が書き換えた等)・
#   old_string にあった行を同じ文字のまま別の場所へ動かしただけの行・textlint か範囲の jq が時間の上限で止められたとき
# 分かっていて受ける形: harness が hook の上限で process group に SIGKILL を送ると、runtimeout が別のグループに置いた textlint の node が
#   自分で終わるまで残る (textlint を 10 秒で止めるので、hook 全体が上限を超えること自体を再現できていない。超えるなら上限の配分を見直す)。
#   範囲の jq は「new_string の出現の数 × 行の数」で伸び、同じ行を数百行並べた replace_all のような入力では 3 秒で止められて黙る
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
# リダイレクトごと包む: 読めないファイルでは入力のリダイレクトの失敗が、wc の 2>/dev/null より先に stderr へ出る
size=$( { wc -c < "$file"; } 2>/dev/null ) || exit 0
[ "$size" -le 160000 ] || exit 0
# 行の数でも絞る: textlint の時間は大きさより行の数で伸びる (2000 行で 5 秒、1 万行・150KB で 47 秒)
lines=$( { wc -l < "$file"; } 2>/dev/null ) || exit 0
[ "$lines" -le 3000 ] || exit 0
runtimeout="$hook_repo/bin/runtimeout"
[ -x "$runtimeout" ] || exit 0

# 書いた行の番号を鍵にした object を出す ({"all": true} は全部の行)。行は 1 始まり。
# jq の indices と文字列の切り出しはどちらもコードポイント単位。行の区切りは textlint に揃える: CRLF・CR だけ・U+2028・U+2029 を
# 位置を探す前に LF へ寄せる (CRLF のファイルでも new_string は LF で届く。揃えないと textlint と行の番号が食い違って知らせが消える)。
# - 行は「前にある改行の数 + 1」で数える。改行の位置は 1 度だけ集め、出現ごとに二分探索で引く (出現ごとに先頭から数え直すと、
#   短い new_string の replace_all で出現の数の 2 乗で遅くなる)。split の要素数で数えない (空の文字列の split は 0 要素で、先頭が 0 行目になる)
# - old_string にある行は書いた行に数えない。同じ行を old_string より多く書いたら (写し)、増えた分は書いた行に数える。
#   行ごとの数は 1 回の走査で数える (new_string の行ごとに前を数え直すと、行の数の 2 乗で遅くなる)
# - 鍵にした object にするのは重複を除くため (出現の数 × 行の数の配列にすると、知らせを組む段が指摘の数 × その数で遅くなる)
# shellcheck disable=SC2016  # 単一引用符の中は jq のプログラム ($c は jq の変数)。runtimeout を前に置くと shellcheck が jq と見分けない
ranges=$(printf '%s' "$input" | "$runtimeout" 3 jq -c --rawfile c "$file" '
  def nl: gsub("\r\n"; "\n") | gsub("[\r\u2028\u2029]"; "\n");
  ($c | nl) as $c
  | if .tool_name == "Edit" then
      (.tool_input.new_string // "" | nl) as $s
      | (reduce (.tool_input.old_string // "" | nl | split("\n"))[] as $x ({}; .[$x] += 1)) as $oldc
      | ($s | split("\n")) as $nl
      | (reduce range(0; $nl | length) as $k ({seen: {}, ks: []};
          if $nl[$k] == "" then . else
            .seen[$nl[$k]] += 1
            | if .seen[$nl[$k]] > ($oldc[$nl[$k]] // 0) then .ks += [$k] else . end
          end) | .ks) as $ks
      | if $s == "" or ($ks | length) == 0 then {lines: {}} else
          ($c | indices("\n")) as $breaks
          | {lines: (reduce ($c | indices($s)[]) as $i ({};
              ($breaks | bsearch($i)) as $b | ((if $b < 0 then -1 - $b else $b end) + 1) as $l
              | reduce $ks[] as $k (.; .[($l + $k) | tostring] = true)))}
        end
    else {all: true} end' 2>/dev/null) || exit 0
[ "$ranges" != '{"lines":{}}' ] || exit 0

# textlint と ja-writing lint は別々に黙る (片方が失敗しても、もう片方の知らせは出す)
notices=()
out=$(JA_TEXTLINT_NO_INSTALL=1 "$runtimeout" 10 "$lint" --ignore-path /dev/null -f json "$file" 2>/dev/null)
rc=$?
if [ "$rc" -le 1 ]; then
  # 範囲は引数でなくプロセス置換で渡す (大きい範囲を --argjson に渡すと argument list too long で jq が起動できず、黙って知らせが消える)
  tl=$(printf '%s' "$out" | jq -r --slurpfile rs <(printf '%s' "$ranges") '
    $rs[0] as $r
    | .[].messages[]
    | select(.ruleId == "ja-ai-vocabulary" or .ruleId == "ja-bold-not-rendered")
    | select($r.all or ($r.lines[.line | tostring] // false))
    | "\(.line):\(.column) \(.message)"' 2>/dev/null) && [ -n "$tl" ] && notices+=("$tl")
fi

jw_rules='["bold_not_rendered", "translated_term", "calque", "metaphor_verb", "slop_vocabulary"]'
jw_tmp=$(mktemp -d 2>/dev/null) || jw_tmp=""
if [ -n "$jw_tmp" ]; then
  trap 'rm -rf "$jw_tmp"' EXIT
  jw() { # <出力のファイル> <ja-writing lint の引数>... (書いたファイルのフォルダで、ビルドせずに起こす)
    local o=$1
    shift
    # shellcheck disable=SC2016  # 単一引用符の中は zsh -c のプログラム ($1 は zsh の引数)
    (cd "$(dirname "$file")" && "$runtimeout" 1 zsh -c 'source "$1"; shift; go_autobuild_exec --no-build "$1" ja-writing -- "${@:2}"' \
      _ "$hook_repo/bin/lib/go_autobuild.zsh" "$hook_repo/src/ja-writing" lint --json "$@" - <"$file" >"$o" 2>/dev/null)
  }
  jrc=0
  jw "$jw_tmp/a.json" || jrc=$?
  if [ "$jrc" -eq 0 ] && ! grep -qF '"words": {' "$jw_tmp/a.json"; then
    jw "$jw_tmp/a.json" --words "$hook_repo/_claude/skills/ja-writing/words" || jrc=$?
  fi
  if [ "$jrc" -eq 0 ]; then
    jwn=$(jq -r --slurpfile rs <(printf '%s' "$ranges") --argjson keep "$jw_rules" '
      $rs[0] as $r
      | [.findings[] | select(.rule as $x | $keep | index($x))] | sort_by(.line) | .[]
      | select($r.all or ($r.lines[.line | tostring] // false))
      | "\(.line) ja-writing \(.rule): \(.message | split("。")[0])。"' "$jw_tmp/a.json" 2>/dev/null) && [ -n "$jwn" ] && notices+=("$jwn")
  fi
fi

[ "${#notices[@]}" -gt 0 ] || exit 0
# 行の順に混ぜてから 20 件で切る (textlint の指摘が多くても ja-writing の指摘が後ろに隠れない)
notice=$(printf '%s\n' "${notices[@]}" | LC_ALL=C sort -s -n | awk 'NR <= 20 { print } END { if (NR > 20) printf "… ほか %d 件\n", NR - 20 }')

msg="ja-textlint: ${file} の書いた行に、AI が書きがちな語・直訳・比喩・表示されない太字がある
$notice
起きていることの言葉に直す (skill ja-writing の規範 7)。言い換えると意味が変わる所 (定義した用語・引用・画面の文言) は元の語を残す"
jq -n --arg c "$msg" '{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $c}}'
