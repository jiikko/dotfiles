# 650 (test): zundamon-kaisetsu の Go と player.html の間の契約が、検査で守られていない

起票日: 2026-10-07

## 概要

Go (`src/zundamon-kaisetsu`) とプレイヤー (`_claude/skills/zundamon-kaisetsu/templates/player.html`) は次の 2 つを別々に持っていて、
片方だけ変えると画面が黙って壊れるのに、テストは緑のまま通る。

### A. まとめ撮りの `#sheet=` の書式

- Go: `mp4.go:writeMP4` が `行,話し中,口;…` を組み立てる。口の段階 0〜2 は `build.go:vowelMouth` / `loadFaces` (`range 3`)、
  JS は `player.html` の正規表現 `/^#sheet=((?:-?\d+,[01],[012];?)+)$/` と `setMouth` の 3 段階
- 発火条件: 口の段階を 4 つにする / 状態に要素を足す。正規表現が合わないとプレイヤーは通常の画面のまま撮られる
- `writeMP4` の「撮った絵が全部同じなら止める」検査で捕まるかは**未実測** (通常ページを 720 ずつ切ると、切り出す位置ごとに違う絵になり
  「全部同じ」が成立しない、というのはコードを読んでの判断。Chrome での確認はしていない)
- `#sheet=` の書式を突き合わせるテストは 0 件 (grep)

### B. 図解の種類と読むキー

- Go: `show.go:showParsers` (登録表)。JS: `player.html:showAt` の if/else の連なりで、該当なしの else が無い (未知の種類は空の箱になる)
- `show_test.go:TestShowKeysReadByPlayer` は種類のサンプルを 4 件の手書きで持ち、`showParsers` と突き合わせていない。
  キーを読んでいるかは部分文字列 (`"sd."+k` を含むか) で見ているので、`sd.sub` を `sd.subx` に変えても PASS する (監査で実測)

## 対応方針

- `#sheet=` の文字列を作る関数を 1 つにし、テストでテンプレートの正規表現を抜き出して、Go が作る文字列 (口の全段階・行 -1) が当たることを確かめる
- `TestShowKeysReadByPlayer` の種類を `showTypes()` から作る (mermaid は image に置き換わるので除く)。キーの照合は語の境目つきの正規表現にする
- `showAt` に未知の種類を見えるようにする else を足す (表にするかは 655 で判断)

## 関連ファイル

- `src/zundamon-kaisetsu/mp4.go` / `build.go` / `show.go` / `show_test.go` / `_claude/skills/zundamon-kaisetsu/templates/player.html`
- 監査の記録: issue 656

## 進捗

- [x] A の固定 — test(zundamon-kaisetsu) の commit
- [x] B の固定 — 同じ commit

## 結果 (2026-10-07)

- A: 口の段階の数を `build.go` の定数 `mouthLevels` に寄せ (`loadFaces` も使う)、`#sheet=` の断片は `mp4.go:sheetFragment` 1 つで作る。
  `TestSheetFragmentMatchesPlayer` が player.html の正規表現を抜き出し、行 -1 / 0 / 12・話し中 0/1・口の全段階の断片を当てる。`vowelMouth` の値が段階の範囲内かも見る
- B: `TestShowKeysReadByPlayer` のサンプルが `showTypes()` (mermaid を除く) を網羅しているかを見る。キーの照合は語の境目つき。
  player.html の `showAt` に未知の種類を「(未知の図解: …)」と見せる else を足し、テストでその分岐があることも見る
- 変異で red: 口の段階を 4 に (golden のテスト 2 本も落ちる) / `sd.sub` を `sd.subx` に / 図解の種類を足す / 未知の種類の else を外す / `#sheet=` の区切りを変える
- 未実測のまま: 正規表現が合わないときに writeMP4 の「全部同じ絵」の検査で捕まるか (Chrome が要る)。食い違いはこのテストで先に落とすので、捕まるかどうかに頼らない

## 敵対的レビュー (2026-10-07、opus、2 周。651 と合わせて)

- 1 周目 P2 2 / P3 3: writeMP4 が sheetFragment を経由しなくても落ちない (偽の Chrome が player.html の正規表現で断片を検査するよう直した) /
  分解の順を見ていない (字面で固定) / キーの照合がコメントや別の種類の分岐に当たる (コメントを除き、種類の分岐ごとに照合) / doc コメントの分断
- 2 周目 P3 3: 分岐の切り出しが showAt の外の helper から始まる (showAt の本体に限る・分岐は 1 つ) / 正規表現の抽出を ^#sheet=…$ の 1 つに限る /
  **検出しない形として記録**: 字面を残して意味だけ変える書き換え (map の中で並べ替える・paint の引数の順を変える)。確実に見るには JS を node で評価する設計変更が要る
