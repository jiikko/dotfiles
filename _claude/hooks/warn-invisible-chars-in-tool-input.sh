#!/usr/bin/env bash
# PreToolUse(Bash / Write / Edit) フック: 道具の入力に見えにくい字 (全角空白・NBSP・ゼロ幅の字・BOM・双方向の制御など) があれば、
# 「\uXXXX の表記のつもりなら、届く前に字に変わっている」と知らせる。
#
# なぜ: 入力に書いた「バックスラッシュ 1 つ + u + 16 進 4 桁」は、道具に届く前に字そのものへデコードされる
# (Bash の単一引用符の中でも同じ。2026-11-10 に od で実測)。書き手は表記を書いたつもりなので確かめる動機が無く、
# rule を知っていても 1 session で 3 回起きた。見えない字は diff でも気づけず、表記で探す検査 (grep・変異の置換) だけが外れる。
#
# 🚨 **止めない (additionalContext だけ)**。その字を意図して書くこともある (日本語の文の全角空白・絵文字の ZWJ・NBSP を扱うテスト)。
# 🚨 **字の一覧はコードポイントの数値で持つ**。このファイル自身を道具で書くので、表記で書くとこのファイルの中で字に変わる。
#
# 鳴らす範囲: 過去の transcript の Bash・Write・Edit の入力のうち、最初の一覧 (全角空白・NBSP・SHY・ZWSP/ZWNJ/ZWJ・LS/PS・WJ・BOM) の字を含んだのは
#   42,318 回のうち 42 回、後で足した字は 42,474 回のうち 0 回 (2026-11-10 に数えた)。毎回鳴って読み飛ばされる頻度ではない。
#   U+FE0F (絵文字の異体字セレクタ) は 423 回あり、絵文字の一部として普通に書くので一覧に入れない。
# Edit は new_string のうち、old_string より数が増えた字だけを見る (元からある字を書き直しただけでは鳴らない)。
# 「該当」は字ごとに前から 3 か所までの出現の周り (同じ行の中の前 40 字・後ろ 39 字) を、合わせて 5 か所まで出す。
#   行を単位に「字が増えた行」を探さない: Edit で行を書き換えた形・元からある字が多い形で、増えた行を当てられなかった。
#   大きな入力で、行の集合・行番号の累積が所要を 5〜10 倍にした
#
# 検出しない形 (silent): 一覧に無い字に変わった表記 (u0041 の表記が A になる・U+FE0F など)・NotebookEdit・jq が無い環境 (fail-open)
# 分かっていて受ける形: 表記から変わった字と、初めから字で書いた字を区別できない (hook が受け取るのは変わった後の入力)。
#   知らせは「字が入っている」までしか言わない。Edit で字を 1 つ消して別の場所に同じ字を足すと、数が同じなので鳴らない。
#   Edit の「該当」は元からある字の周りも出る (増えた字の場所は特定しない。要約の数は増えた分だけ)

set -uo pipefail

command -v jq >/dev/null 2>&1 || exit 0

jq -c '
  def hex4: . as $n | [12, 8, 4, 0] | map(($n / pow(2; .) | floor) % 16) | map("0123456789ABCDEF"[.:.+1]) | join("");
  # 空の文字列の split は [] を返す (要素 1 つにならない) ので分ける
  def count($ch): if . == "" then 0 else (split($ch) | length) - 1 end;
  # 字の位置を文字列の index で取らない: jq 1.7 の index はバイトの位置を返す (macOS の /usr/bin/jq は 1.7.1)。split と、字の単位で切る slice を使う
  ([ [160, "NBSP"], [173, "SHY"], [847, "CGJ"], [4447, "ハングルの埋め字"], [4448, "ハングルの埋め字"], [6158, "MVS"],
     [8203, "ZWSP"], [8204, "ZWNJ"], [8205, "ZWJ"], [8206, "LRM"], [8207, "RLM"], [8232, "LS"], [8233, "PS"],
     [8239, "NNBSP"], [8287, "MMSP"], [8288, "WJ"], [12288, "全角空白"], [12644, "ハングルの埋め字"], [65279, "BOM"],
     [65440, "ハングルの埋め字"] ]
   + [range(8192; 8203) | [., "幅の違う空白"]]
   + [range(8234; 8239), range(8294; 8298) | [., "双方向の制御"]]
   + [range(8289; 8293) | [., "見えない演算子"]]) as $list
  | (.tool_input // {}) as $in
  | (if .tool_name == "Bash" then ($in.command // "")
     elif .tool_name == "Write" then ($in.content // "")
     elif .tool_name == "Edit" then ($in.new_string // "")
     else "" end) as $s
  | (if .tool_name == "Edit" then ($in.old_string // "") else "" end) as $old
  | [ $list[] | ([.[0]] | implode) as $ch
      # 数えた split の結果 (p) を周りの切り出しにも使う (大きな入力で split を 2 回しない)
      | {c: .[0], name: .[1], ch: $ch, p: (if $s == "" then [] else ($s | split($ch)) end), m: ($old | count($ch))}
      | .n = ([(.p | length) - 1, 0] | max)
      | select(.n > .m)
      | .label = "U+" + (.c | hex4) + " (" + .name + ")" ] as $hits
  | select($hits | length > 0)
  # 字の周りは、その字で split した前後の部分 (それぞれ 3 つまで) を繋いでから切る。全体を join しない (出現の多い大きな入力で遅くなる)
  | ([ $hits[] | . as $h | $h.p as $p
      | range(0; [($p | length) - 1, 3] | min) as $k
      | (($p[([$k - 2, 0] | max):($k + 1)] | join($h.ch)) | .[-40:] | split("\n") | last // "") as $before
      | (($p[($k + 1):($k + 4)] | join($h.ch)) | .[:39] | split("\n") | first // "") as $after
      | ($before + $h.ch + $after) | gsub("\r"; "")
      | reduce $hits[] as $x (.; split($x.ch) | join("<" + $x.label + ">")) ]
    | reduce .[] as $w ([]; if index([$w]) then . else . + [$w] end) | .[:5] ) as $lines
  | ($hits | map(.label + " x" + ((.n - .m) | tostring)) | join(", ")) as $summary
  | "道具の入力に見えにくい字がある: " + $summary + "。"
    + "\\uXXXX の表記を書いたつもりなら、道具に届く前に字に変わっている (バックスラッシュ 1 つ + u + 16 進 4 桁はデコードされる。Bash の単一引用符の中でも同じ。2 つ書くと 2 つのまま届く)。"
    + "表記を残すなら、python の chr(0x5c) か printf の \\x5c でバックスラッシュを組む。その字を書くつもりならこのままでよい。\n該当 (最大 5 か所):\n"
    + ($lines | map("  " + .) | join("\n"))
  | {hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: .}}
' 2>/dev/null || exit 0
