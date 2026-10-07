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

- [ ] A の固定
- [ ] B の固定
