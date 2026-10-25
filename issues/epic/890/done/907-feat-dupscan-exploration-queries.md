# 907 (feat): 探索の問い合わせ (outline・find・callers) で、grep より文脈を食わずに関連の実装を探す

起票日: 2026-10-24

## 概要

ユーザー (2026-10-24): dupscan は、重複の検出だけでなく、今まで grep でしていた普通のコーディングの「関連の実装を探す」調査にも使えないか。
文脈の消費を減らして作業を進められないか。

grep で定義を探すと、呼び出し側の行・コメント・文字列の一致が混ざり、1 行では署名が分からずファイルを開いて読むことになる。
dupscan は構文木で単位を取り出しているので、**定義だけを 1 件 1 行** (名前・署名・file:line) で返せる。doc の 1 行目は今の adapter がコメントを捨てているので、
索引 ([897](897-feat-dupscan-index-subcommand.md)) で doc を取り出してから足す。`find` は 897 の引き方 (名前・署名の型・doc 文) の入口で、別の実装を持たない (入口を 2 つにしない)。

| subcommand (名前は仮) | 問い | 返すもの |
|---|---|---|
| `outline <file>` | このファイルに何があるか | 単位 (関数・初期化子・computed property・subscript など) の署名を 1 行ずつ。型・stored property・enum の case・protocol の要件は今は単位でないので、出すなら別に取り出す。テスト関数は単位にしないので、test のファイルでは別の扱いが要る |
| `find --name <語>` / `--returns <型>` / `--param-type <型>` | 名前・戻り値・引数の型で定義を探す | 一致した定義だけを 1 行ずつ (呼び出し側は出さない) |
| `callers <名前>` | どこから呼ばれているか | 呼び出しの箇所 (名前と引数ラベルで絞る構文だけの近似。893 の呼び出しの箇所の抜き出しを使う) |

## 限界 (README に書く)

- 型を解決しないので、同名のメソッドの区別・extension をまたぐ呼び出し元は近似 (891 の「LSP は使わない」)
- 文字列・コメント・設定ファイルを探すのは grep のほうが向く
- 対応する言語 (最初は Swift) のファイルだけ

## 受け入れ条件

- [x] 上の 3 つの問い合わせ。出力は短くし、件数に上限を置く (Claude が何度も引く)。`--json` も出す
- [x] 効果を測る: DualNoteApp で、同じ問い (例: 「Memo を返す関数」「このファイルの中身」「X の呼び出し元」) を grep と dupscan で引き比べ、
      Claude に返る文字数と、目的の定義に辿り着くまでに読んだファイルの数を並べて書く
- [x] 903 (書く前に探す手順) と、使う側の repo の入口に、調べるときに grep の前に引く手順を書く

## 依存

- [893](893-feat-dupscan-core-normalize-type2.md)・[894](894-feat-dupscan-swift-adapter.md) (単位の一覧・呼び出しの箇所)・[897](897-feat-dupscan-index-subcommand.md) (索引と同じ一覧)

## 進捗

- 2026-10-24 起票 (ユーザーの問い: 重複の検出ではなく、関連の実装の調査にも使えないか)
- 2026-10-24 2 周目の敵対的レビューの反証を取り込んだ: doc の 1 行目は 897 を待つ・`find` は 897 の引き方の入口で別の実装を持たない・`outline` が出せるのは今の単位だけ
- 2026-10-25 `outline` と `find` は 897 の `dupscan index` の引き方として作った (入口を 2 つにしない): `outline <file>` = `index -file <file>` (行の順・件数で切らない)、
  `find --name / --returns / --param-type` = `index -name / -returns / -param-type` (doc の語は `-doc`)。DualNoteApp で `-returns Tag` が 27 件を 1 行ずつ返し、`-file` が outline を返すことを確かめた (書き込み 0)。
  **残り**: `callers` (呼び出しの箇所の抜き出し)・効果の測定 (grep と dupscan で同じ問いを引き比べ、返る文字数と読んだファイルの数)・903 と使う側の入口への手順
- 2026-10-25 効果の測定 (DualNoteApp・同じ問いを grep と `dupscan index` で引いた出力の文字数。grep は `--include='*.swift'`、index は `-limit 0`):

  | 問い | grep | index |
  |---|---|---|
  | `[Tag]` を返すもの | `Tag\]` 4271 字 45 行 25 ファイル / 定義に絞った正規表現 `func .*-> \[Tag\]\|var .*: \[Tag\]` 2606 字 27 行 | `-returns '[Tag]'` 1386 字 13 行 |
  | 名前に memo を含む関数 | `Memo` 214296 字 1949 行 92 ファイル / `func [a-z]*memo` (-i) 16615 字 146 行 | `-name memo` 9193 字 70 行 |
  | このファイルに何があるか (TagPicker.swift) | `cat` 1978 字 65 行 | `-file` 232 字 3 行 |

  grep の定義に絞った正規表現も、stored property・テスト関数・呼び出しの行が混ざる (`[Tag]` の 27 行のうち、戻り値が `[Tag]` の単位は 13)。index は既定で上位 20 件に切るので、実際に Claude に返るのはさらに短い。
  「目的の定義に辿り着くまでに読んだファイルの数」は、Claude の実際の作業で測る必要があるので未測定 (trigger: 903 の手順を入れて、使う側の repo で調べものをしたとき)

- 2026-10-25 `callers` を作った (commit「feat(dupscan): 呼び出しの箇所を引く callers」): adapter がファイルの呼び出し (`call_expression`) を全部集め (テスト関数の中も)、
  呼ばれる名前と括弧の中の引数ラベル・後置クロージャの有無を持たせる。`dupscan callers 名前` か `名前(ラベル:…)` で引き、`file:line 呼び出し元 [test] | その行` で出す。
  呼び出し元は名前の付いた単位の最も内側 (クロージャの中なら囲む関数)。並べ方は prod → test → 場所。
  文法が `a + b.f(x)` を「`a + b.f` を呼ぶ」と読むので、二項式・前置の式は右端の式へ降りて名前を取る (DualNoteApp ではなく fixture の 7 行目で見つけた)。
  テスト 3 本、変異 8 本 (二項式へ降りない・後置クロージャで補う条件を外す・ラベルの数を比べない・prod / test の並びを外す・クロージャを呼び出し元にする・ラベルを比べない・
  ラベルを 1 つしか読まない・閉じない括弧を通す) はビルドが通り全部赤。DualNoteApp で `callers mergedTags` が 5 件 (0.6 秒)、`callers 'add(name:color:to:)'` が 1 件 (書き込み 0)。
  **残り**: 効果の測定のうち「読んだファイルの数」(上の trigger)・903 と使う側の入口への手順。型を解決しない近似のまま (891 の「LSP は使わない」)
- 2026-10-25 done: `outline` / `find` は `index -file` / `index -name・-returns・-param-type` として、`callers` は別の subcommand として作った。返る文字数の引き比べは済み。目的の定義に辿り着くまでに読んだファイルの数は 917 の検証 1 (grep / Read の回数) で測る。手順は skill `dupscan` に書いた (903)
