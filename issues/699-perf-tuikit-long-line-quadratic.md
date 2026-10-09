# 699 (perf): tuikit の highlight と markdown が、1 行・1 段落が長いと 2 乗で遅くなり UI が固まる

起票日: 2026-10-09

## 概要

入力の長さに上限が無く、長さに対して非線形の処理 (第三者の lexer・自前の走査) に渡している。どちらも黙って固まる。

## 詳細

1. **`highlight.codeLine` に 1 行の長さの上限が無い** (P2) — chroma の lexer が言語によって 2 乗で伸びる。`Lang` で実測: TypeScript の日本語 8,000 字 1.8 秒・
   16,000 字 9 秒、JavaScript 32,000 字 12 秒、sh の base64 20,000 字 3.6 秒・40,000 字 21 秒、TypeScript 80,000 字 2 分 19 秒 (css / json / go / python は
   80 KB でも 0.5 秒未満)。影響:
   - treefiler のタイル (`filer/tileview.go` の view。`ForPath` 経由) は UI の goroutine で呼ぶので**画面が固まる** (1 行 16 KB の上限があっても TS の日本語で 9 秒)
   - glogx の `highlight.Diff` は行数を 5000 で切るが 1 行の長さは切らない。pro-con の diffview も同じ (裏の cmd の中)
   - markdown のフェンスのコードも同じ経路
   直し方: codeLine で一定の長さ (4 KB 程度) を超えたら素通しにする (`lex == nil` と同じ扱い)
2. **markdown の parseInline が 2 乗** (P2) — `markdown/inline.go` の matchDelim / matchLink。閉じない `*` `**` `~~` `[` が 1 段落に多いと: `"*a "×40,000`
   (120 KB) 7.3 秒・`"~~a "×40,000` 8.9 秒・`"[a]("×40,000` 2.9 秒。段落の改行を `reflowJoin` で毎行 `prev+" "+next` と連結するのも 2 乗 (`"x\n"`×200,000 で 4.2 秒。反証レビューの再測。起票時の 0.63 秒は入力が違った)。
   highlight の時間は反証レビューの再測で TS の日本語 8,000 字 1.27 秒・16,000 字 4.96 秒 (マシン差。2 乗で伸びるのは同じ)
   issue の本文は第三者が書く前提で、Render に長さの上限は無い。直し方: 「閉じが無い」を段落ごとに 1 回の探索で覚える・reflowJoin を Builder にする
3. 契約: 「Render / Lang は 1 行・1 段落の長さを上限で切る」を tuikit の README に書けば、使う側ごとに上限を持たなくてよい

## 関連

- treefiler の 693 (巨大なフォルダ) とは別の軸。監査の記録: 702

## 進捗

- [ ] 未着手
- [x] 1 `highlight.codeLine` は 1 KiB を超える 1 行を色を付けずに返す (Diff・Lang・ForPath の全経路)。上限ちょうどの 1 行の所要: makefile 57 ms
  (いちばん遅い。2 KiB で 166 ms・4 KiB で 650 ms なので 4 KiB から下げた)・TypeScript / Python 10〜20 ms・ほかは数 ms。repo の git 管理下で
  1 KiB を超える行は 47.8 万行中 56 行 (PDF・散文。.go は 0。反証レビューで計数)
- [x] 2 markdown: `parseInline` を `inlineScan` にし、括弧の対応 (スタックで 1 回) と強調の閉じ (区切りごとに後ろから 1 回。旧来の
  「無効な出現は区切りの長さだけ飛ばす」走査と同じ答え) を前計算する。強調・リンクの中身は同じ表のまま区間を狭めて読み、style とリンクは
  外から渡して span を入れ子のたびに歩き直さない。`mergeSpans` の `+=` と段落の連結 (`reflowJoin` → `reflower`) は Builder に。
  実測: `*a `×40,000 7.3 秒 → 6 ms・`~~a `×40,000 8.9 秒 → 7 ms・`[a](`×40,000 2.9 秒 → 6 ms・`x\n`×200,000 4.2 秒 → 38 ms (M 系 Mac)
- [x] 3 tuikit の README に契約を書いた (markdown は 1 段落の時間が長さに線形・highlight は 1 KiB を超える行を素通し)
- 旧実装 (456ebbfa) を正解役にした突き合わせ: matchLink / matchDelim を 385 万件、Render と RenderLinks の出力を 31 トークン・長さ 1〜24 の
  30 万件 (リンクを含む記録 35 万) で比べて差 0。反証レビューも 39 種のトークン・400 万件で差 0
- 変異 (8 本 red): 上限を外す / 上限の境界を >= にする / 閉じの表の使い回しを止める / 段落の連結を作り直しに戻す / 中身の再帰で表を
  作り直す / 入れ子のリンクのたびに中の span を歩き直す (最初は入力が小さく緑だった → 深さと span の数を 20 万にして red)。所要の検査は
  `TestRenderPathologicalParagraphsAreLinear` の 10 秒の hang guard (実時間の合否ではない)
- 敵対レビュー (sonnet、2 周): 1 周目 P2 2 件 (入れ子のリンク・リンクの中の強調で表を作り直して 2 乗、旧より遅い) → 表の引き継ぎで直した。
  2 周目 P2 1 件 (入れ子のたびに withLink が中の span を歩き直す) → style とリンクを外から渡す形で直した。P3 (makefile の lexer が上限ちょうどで
  650 ms) → 上限を 1 KiB に。2 周目の修正は正解役との突き合わせと変異で確かめ、3 周目は回していない
- `make test` / `make lint` (src/tuikit) rc=0。glogx・pro-con・treefiler の go test rc=0
