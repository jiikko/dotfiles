# 278 human: glogx の issues view で epic group の折り畳み表示を目視確認する

起票日: 2026-09-06
期限: 2026-09-12
種別: human

## 確認すること (obaket 719 R4 の「実機の目視は未」)

obaket (`~/src/my-products/apps/obaket`) で glogx の issues view を開き:

1. 親行「▸ google-drive (N)」「▸ obaketcloud (N)」が出て、展開 / 折り畳みが動く
2. group 内の issue を `n` で group 内 `next/` へ移せる (global `issues/next/` に行かない)
3. 親行で番号 filter を Esc したとき添字が残らない (round 3 で直した箇所)

### 2026-09-06 追記 (issue 291 の実装ぶん)

4. group を展開すると、**`a` を押していない既定の状態でも** `epic/<name>/done/` の子が `✓` で、
   `epic/<name>/pending/` の子が `⏸` で見える (global の done/pending は従来どおり `a` を進めるまで見えない)
5. 親行が `▸ <name> (5 ✓2)` の形になる (done が 0 件の group は従来の `(N)` のまま)。
   **半角と全角が同じ桁に並んでいないか、実際の幅で目視する**
6. `epic/<name>/closed/` のように予約外の綴りのディレクトリに md を置くと、迷子 `?` として
   一覧に出る (消えない)

## 結果

(確認したら書いて done へ)
