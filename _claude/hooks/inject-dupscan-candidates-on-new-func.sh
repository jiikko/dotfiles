#!/usr/bin/env bash
# PreToolUse(Write / Edit) フック: 新しい関数 (Go / Swift) を足す書き込みの直前に、同じ名前の既存の定義を dupscan index で引いて注入する (push 型)。
#
# なぜ: 「書く前に dupscan を引け」と案内するだけでは、agent は呼ばない (30 日で識別子検索 1,680 回に対し dupscan の探索 2 回)。
# 呼ぶかどうかを agent の判断に任せると効果が出ない。書く直前に候補を目の前へ出す。
#
# 🚨 止めない (additionalContext だけ)。permissionDecision を返さない。どんな失敗も無音で rc=0 (fail-open)。
# 🚨 出しすぎない: session × 名前で 1 回。候補が無ければ何も出さない。名前の一致だけを出す (部分一致の雑音は出さない)。
#    引いた印は ${XDG_CACHE_HOME:-~/.cache}/claude-dupscan-inject.seen の 1 ファイルに「session 種類 名前」の行で持ち、上限 (hooks/lib/session_marks.py の MAX_BYTES) を超えたら古い半分を捨てる。
#    名前ごとの空ファイルを $TMPDIR に作らない (消す者が居らず、注入のたびに 1 件ずつ溜まる)。捨てた行の session × 名前は、もう 1 回注入されうる
#    戻り型が取れたら、名前と戻り型の組み合わせ (index -name 名前 -returns 型) で先に引く。名前だけだと、同じ名前で別の型を返す定義が上位を埋めて
#    上限の外へ落ちる。名前の照合の後に残りの予算で引く。汎用の型 (error・string・bool・数値・Self など。GENERIC_RETURNS) では型で引かない。引数型では引かない (string や context が多く雑音になる)
# 🚨 遅延: 名前 1 つにつき dupscan index を最大 4 回 (名前・語の前方 2 つ・戻り型。全体で 6 秒の予算。超えたら残りは引かず、時間切れの問い合わせは空として続ける)。
#
# 判定 (python):
#   - file_path が .go / .swift。dupscan と git が在る。repo の root は書くファイルの属する git repo
#   - 新しい関数 = 書いた後のファイル (Write は content、Edit は今のファイルの old_string を new_string に置き換えたもの) にある
#     `func name(` のうち、Edit の old_string にも、書き換え前のファイルにも `func name(` が無いもの。書いた後のファイルを作れない
#     (ファイルが無い・old_string が見つからない) ときは new_string だけを見る
#   - 文字列リテラルとコメントの中は見ない (Go の raw string・"…"・rune、Swift の """…"""・"…"、// と /* */)。test が Go のソースを
#     fixture として文字列に持つと、その中の func を新しい関数と読んでいた。Edit の new_string は文字列の途中から始まりうるので、
#     書いた後のファイル全体を頭から読んで決める
#   - git が ignore するパス (tmp/ の使い捨ての道具など) では引かない
#   - 除く名前: 5 文字未満・Test/Benchmark/Fuzz/Example 始まり・init/main/String/Error/Close/body ほか決まり文句、Go のメソッド (func (r T) name)
#   - 候補 = index の行のうち、書くファイルと同じ拡張子のファイルにあり、名前が大小無視で等しいもの、または一方が他方の前方で共通部分が 6 文字以上で、短い方の名前が 2 語以上のもの
#     (短い方が 1 語 (`resume`) だと、`resumeReady` に対して語が同じだけの無関係な `resume()` が並ぶ。失うもの: 1 語の既存の関数
#     (`upload`) を、それに語を足した新しい名前 (`uploadAll`) で書くときは出ない)
#   - 新しい型 = 書く文字列にある型の宣言 (Swift の class / struct / actor / enum / protocol、Go の行頭の `type 名前`) のうち、書き換え前に無いもの。
#     同じ言語のファイルに同じ名前の型の宣言があれば (private を含む) 出す。git grep の字句の完全一致で、dupscan は使わない
#     (index は型を返さず、Go では def が使えない)。型は計測ログに書かない (ログは関数の注入の A-B を数える)
# 検出しない形 (silent): Bash の書き込み (commit の後に inject-dupscan-candidates-on-commit.sh がその commit の差分から拾う)・NotebookEdit・名前が違うが同じ役割の関数 (戻り型は名前と組み合わせて引くだけで、型だけでは引かない。dupscan search / index -returns で引く)・
#   言語の混ざる repo で、違う言語の候補が上限 (-limit) を埋めて同じ言語の候補が落ちる形 (index は言語で絞れない。落ちたら何も出ない側に倒れる)・
#   Swift のメソッドと Go の関数の区別 (Swift は字下げのある func も新しい関数として扱う)・Go の `type (` の中の型・
#   引数付きの属性 (`@available(…)`) の付いた Swift の型・typealias
# 計測: 注入のたびに ${XDG_CACHE_HOME:-~/.cache}/claude-dupscan-inject.log へ「時刻 session 名前 候補数 引き方 (name / returns) 入口 (edit)」を 1 行足す
#   (scripts/dupscan_usage_report.py が数える)。印のファイルを読み書きできないときは「時刻 warn 理由」の 3 列の行も足す

set -uo pipefail
unset CDPATH

# 実験用の opt-out: DUPSCAN_INJECT=off の run は注入しない (A-B で処置の有無だけを変えるため。本番の既定は on。名前の候補の判定は変えない)
[ "${DUPSCAN_INJECT:-}" = off ] && exit 0
command -v python3 >/dev/null 2>&1 || exit 0
command -v git >/dev/null 2>&1 || exit 0
command -v dupscan >/dev/null 2>&1 || exit 0

lib="$(cd "$(dirname "$(readlink -f "$0")")" && pwd -P)/lib"
# 判定の本体は lib/dupscan_inject.py (commit の後の入口 inject-dupscan-candidates-on-commit.sh と共有する)。
# exec しない: lib の読み込み・構文の失敗も rc=0 で黙らせる (fail-open)
python3 -I -B "$lib/dupscan_inject.py" edit "$lib" || true
exit 0
