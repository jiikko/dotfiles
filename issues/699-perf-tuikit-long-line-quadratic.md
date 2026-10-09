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
