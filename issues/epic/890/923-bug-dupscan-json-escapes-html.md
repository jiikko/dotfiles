# 923 (bug): dupscan の JSON の出力が `<` `>` `&` を `<` `>` `&` に escape する

> 🚨 **担当中: M4Pro-MacBook-Pro (glogx)**（2026-10-26〜）

起票日: 2026-10-26

## 概要

dupscan の JSON の出力 (report / gate / near / def / refs / callers / index の `-format json`、`-v` の「除外の 1 行」、効いていない除外の行、gate の記録) が、
Go の `encoding/json` の既定で `<` `>` `&` を escape している。JSON として読めば同じ値だが、人が読む・貼る・grep するときに困る。

## 詳細 (再現)

- `dupscan report -v` の「除外の 1 行」: `static location(_ connection: StorageConnection, path: String) -> StorageLocation@b2522f088f64`。
  この 1 行は `.dupscan.json` に貼るためのもの。obaket の `.dupscan.json` は `->` が 323 か所・`>` が 0 か所 (2026-10-26 に数えた) で、
  貼る側が毎回手で戻している (obaket issue 1305 でも python で書き直した)。戻さずに貼ると、`->` で grep しても当たらない除外が混ざる
- `def -format json` の本体の `&&` は `&&`、`Array<Int>` は `Array<Int>` になる

## 対応方針

JSON を書く 4 か所 (`report.WriteJSON`・`exclude.GroupSnippet`・`report.ruleJSON`・`report.AppendRecord`) を、escape しない 1 つの helper に通す (`json.Encoder.SetEscapeHTML(false)`)。
JSON として読んだ値は変わらない。

## 受け入れ条件

- [ ] 4 か所とも `<` `>` `&` をそのまま出す。テストで固定し、helper の `SetEscapeHTML(false)` を外す変異で赤を確かめる
- [ ] JSON として読んだ値が変わらない (obaket の `report -format json -limit 0 .` を変更前と JSON で比べて一致)
- [ ] 1 行で出す所 (除外の 1 行・gate の記録) は改行を含まない (Encoder が足す末尾の改行を落とす)

## 進捗
