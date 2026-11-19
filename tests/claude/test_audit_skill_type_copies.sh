#!/usr/bin/env bash
#
# audit skill の監査タイプの ID が、SKILL.md の中の写しの間で食い違っていないことを検査する。
#
# なぜ: タイプの一覧は 1 か所に寄せられない。AskUserQuestion に出すカテゴリ・forge に渡すプロンプト・
# 直接実行の指針は、読む場面が違うのでそれぞれの節に ID ごとの行を持つ。タイプを足す・消すときに
# 1 か所を直し忘れると、そのタイプは選べない・forge で回せない・指針なしで回る (どれもエラーを出さない)。
#
# 正本: 「### 監査タイプ一覧」の表の ID 列。検査する写し:
#   1. 「### 選択方法」のカテゴリの行 (`- 設計 (design, ...)`) の ID の和 = 表の ID の集合
#      各カテゴリの行の ID = 表でそのカテゴリに属する ID (設計 / 品質 / テスト。その他は残り全部)
#   2. 「### forge モードが選択された場合」のプロンプトの行 (`- **<id>**: \``) = 表の ID の集合。
#      `[モード名]` で始まるプロンプトの行も Step 3 の選択肢 `- forge（` もどこにも無いときは検査しない (forge の経路ごと消すのは追随漏れではない)。
#      どちらかが残っているのに節が見つからないときは落とす (見出しの書き換えや、選択肢を残してプロンプトだけ消す形で写し 2 が黙って外れないように)
#   3. 「### 直接実行」の指針の行 (`- **<id>**: `) = 表の ID の集合
#   4. 「設計系 (design / ... ) を直接実行でまとめて回すときは」の括弧の中の ID = 表のカテゴリ「設計」の ID
#
# 検出しないと決めた形:
#   - 発見の型 (L1〜L7 / E1〜E4 ...) の写し。型の記号はタイプの中で閉じていて、別の節に並べ直す写しが無い
#   - 説明・名前の文の食い違い (表と指針で説明が違っても、ID が揃っていれば選べて回せる)
#   - 散文の中で ID に触れている箇所 (寄せ先の説明など)
#   - 節を同じ深さの見出し (`###`) で分けること (節がそこで終わり、後ろの行が「無い」として赤になる。分けるなら `####` にする)
#   - skill-eval の grader (`_claude/skill-evals/audit/`)。CI では回さない別の検査が持つ

set -euo pipefail
unset CDPATH  # CDPATH が export されていると `cd foo` が解決先を stdout に出し、SCRIPT_DIR が 2 行に化ける

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
SKILL_MD="${AUDIT_SKILL_MD:-$ROOT_DIR/_claude/skills/audit/SKILL.md}"

[ -f "$SKILL_MD" ] || { echo "FAIL: 検査対象が存在しない: $SKILL_MD" >&2; exit 1; }

fail=0
checked=4

# $1 の見出し (行全体が一致) から、次の同じ深さ以上の見出しの手前までを出す
section() {
  awk -v h="$1" '
    /^```/ { fence = !fence }
    !fence && $0 == h { on = 1; n = length($0) - length(substr($0, index($0, " "))); next }
    on && !fence && /^#+ / { d = index($0, " ") - 1; if (d <= n) exit }
    on { print }
  ' "$SKILL_MD"
}

sorted() { awk '{ gsub(/[ ,\/]+/, "\n"); print }' | grep -v '^$' | LC_ALL=C sort; }

# 抽出は `|| true` を付ける (無マッチの grep で set -e が代入ごと落とし、理由の無い失敗になるため)。
# 空を受けたら need が名指しで落とす
need() { [ -n "$1" ] || { echo "FAIL: $2 を抽出できない (書式が変わった?)" >&2; exit 1; }; }

compare() {  # $1=写しの ID (改行区切り・整列済み) $2=正本 $3=説明
  if [ "$1" != "$2" ]; then
    echo "FAIL: $3 が表と食い違う" >&2
    diff <(printf '%s\n' "$2") <(printf '%s\n' "$1") | sed -n 's/^< /  表にだけある: /p; s/^> /  写しにだけある: /p' >&2 || true
    fail=1
  fi
}

table=$(section '### 監査タイプ一覧' | awk -F'|' 'NF > 3 { id = $2; c = $3; gsub(/ /, "", id); gsub(/ /, "", c); if (id != "ID" && id !~ /^:?-+:?$/) print id "\t" c }' || true)
need "$table" "表の ID とカテゴリ"
all=$(printf '%s\n' "$table" | cut -f1 | LC_ALL=C sort)
dups=$(printf '%s\n' "$all" | uniq -d)
[ -z "$dups" ] || { echo "FAIL: 表に同じ ID が 2 回ある: $dups" >&2; fail=1; }

# 1. カテゴリの行
sel=$(section '### 選択方法')
cat_ids() { printf '%s\n' "$sel" | sed -n "s/^- $1 (\\(.*\\)) *\$/\\1/p" | sorted || true; }
union=""
for c in 設計 品質 テスト その他; do
  got=$(cat_ids "$c")
  need "$got" "選択方法の「$c」の行"
  union=$(printf '%s\n%s\n' "$union" "$got")
  if [ "$c" != その他 ]; then
    compare "$got" "$(printf '%s\n' "$table" | awk -F'\t' -v c="$c" '$2 == c { print $1 }' | LC_ALL=C sort)" "選択方法の「$c」の行"
  fi
done
compare "$(printf '%s\n' "$union" | grep -v '^$' | LC_ALL=C sort)" "$all" "選択方法のカテゴリの行の和"

# 2. forge のプロンプト (節があるときだけ)
forge=$(section '### forge モードが選択された場合')
if [ -n "$forge" ]; then
  got=$(printf '%s\n' "$forge" | sed -n 's/^- \*\*\([A-Za-z0-9_-]*\)\*\*: `.*/\1/p' | LC_ALL=C sort || true)
  need "$got" "forge のプロンプトの行"
  compare "$got" "$all" "forge のプロンプトの行"
elif grep -q '^- \*\*[A-Za-z0-9_-]*\*\*: `\[モード名\]' "$SKILL_MD" || grep -q '^- forge（' "$SKILL_MD"; then
  echo "FAIL: forge のプロンプトの行か Step 3 の forge の選択肢があるのに「### forge モードが選択された場合」の節が見つからない (見出しを変えた? forge を消すなら選択肢も消す)" >&2
  fail=1
else
  echo "note: forge のプロンプトが無いので写し 2 は検査しない"
  checked=3
fi

# 3. 直接実行の指針
direct=$(section '### 直接実行 (not forge) が選択された場合')
got=$(printf '%s\n' "$direct" | sed -n 's/^- \*\*\([A-Za-z0-9_-]*\)\*\*: .*/\1/p' | LC_ALL=C sort || true)
need "$got" "直接実行の指針の行"
compare "$got" "$all" "直接実行の指針の行"

# 4. 設計系をまとめて回す段落の括弧
got=$(printf '%s\n' "$direct" | sed -n 's/^\*\*設計系 (\([^)]*\)) を直接実行でまとめて回すとき.*/\1/p' | sorted || true)
need "$got" "設計系をまとめて回す段落の括弧"
compare "$got" "$(printf '%s\n' "$table" | awk -F'\t' '$2 == "設計" { print $1 }' | LC_ALL=C sort)" "設計系をまとめて回す段落の括弧"

n=$(printf '%s\n' "$all" | wc -l | tr -d ' ')
if [ "$fail" -ne 0 ]; then
  echo "FAIL: audit の監査タイプの写し (表の ID $n 件)" >&2
  exit 1
fi
echo "PASS: audit の監査タイプの写し $checked 種が表の ID $n 件と一致"
