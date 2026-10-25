# 922 (bug): dupscan の def / refs の分かっている限界を直す

> 🚨 **担当中: M4Pro-MacBook-Pro (glogx)**（2026-10-26〜）

起票日: 2026-10-26

## 概要

921 で README の「分かっている限界」に書いた def / refs の誤り 5 つと、obaket で使って気づいた 1 つを直す。ユーザー指示 (2026-10-26)「dupscan にフィードバックできることがあったらやって」。

## 直すもの (再現)

1. **refs で型の直下の宣言の行が「(単位の外)」と出る**: obaket の `refs StorageRequestStatsRecorder` で `DropboxClient.swift:67 (単位の外) [型の位置] | private let statsRecorder: …`。
   関数の外でも型の中なら、囲む型の名前 (`DropboxClient`) を出す (top-level のときだけ「(単位の外)」)
2. **`def count()` (空のラベル) が関数以外にも当たる**: 括弧を書いたら、関数・初期化子・subscript・protocol の関数の要件だけに当てる (property・case・typealias には当てない)
3. **`var x = 0, y = 0` の 2 つ目以降の名前を def で引けない**: 1 つの property 宣言の束縛を全部、定義として集める
4. **複数行の enum の case が 1 行目しか出ない**: `case foo(\n a: Int\n)` の EndLine を case の終わりまでにする
5. **`if let foo {` (Swift 5.7 の省略形) で外側の foo への参照を数えない**: 省略形は外側の同名の値への参照でもあるので、refs で数える (宣言と参照の両方の性質を持つ。数えて [式の位置] と出す)
6. **parameter pack の宣言 (`<each T>` の T) を refs で参照に数える**: 宣言名として除く

## 受け入れ条件

- [ ] 上の 6 つを直し、それぞれをテストで固定する (変異で赤を確かめる)
- [ ] report / gate / index / callers の出力と、単位の鍵・Name・ハッシュが変わらない (obaket で変更前後の report の組が同じ)
- [ ] README の「分かっている限界」から直したものを消す
