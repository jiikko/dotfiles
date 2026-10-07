# 651 (test): zundamon-kaisetsu の mp4 の組み立て (writeMP4) にテストが無い

> 🚨 **担当中: dotfiles-4d**（2026-10-07〜）

起票日: 2026-10-07

## 概要

`src/zundamon-kaisetsu/mp4.go` の `writeMP4` / `encodeAudio` / `h264Encoder` / `pngSize` / `findChrome` をテストから呼んでいる箇所は 0 件
(2026-10-07 に grep)。`cmdBuild` を呼ぶテストは `interrupt_test.go` の 1 件だけで、format は html。そのため次を壊しても全テストが緑になる:

- まとめ撮りのグループ分けと crop の位置の計算
- 撮った絵が全部同じときの検出
- Chrome / ffmpeg の rc と絵の大きさの検査
- 最後の mux の失敗の扱い

プレイヤーの描画そのものに自動テストが無いことは issue 645 で既知 (人が見る)。Go 側の組み立ては、`interrupt_test.go` と同じ偽の chrome / ffmpeg で検査できる。

## 対応方針

- 偽の chrome (指定の大きさの縦長の PNG を書く) と偽の ffmpeg (引数を記録し、出力を作る) で `writeMP4` を通すテストを足す。
  crop の位置・グループ分け・rc の伝わり方・「全部同じ絵」の検出を、引数の記録と rc で判定する
- 足したテストに変異 (crop の位置をずらす / 全部同じの検査を外す) を当てて red を確かめる

## 関連ファイル

- `src/zundamon-kaisetsu/mp4.go` / `interrupt_test.go` (偽のコマンドの作り方)
- 監査の記録: issue 656

## 進捗

- [x] 偽の chrome / ffmpeg で writeMP4 のテスト — test(zundamon-kaisetsu) の commit

## 結果 (2026-10-07)

- `mp4_test.go`: 偽の chrome / ffmpeg はテストのバイナリ自身を `FAKE_MP4_TOOL` つきで起こす。偽の chrome は `#sheet=` の状態ごとに状態を表す色の帯を
  塗った縦長の PNG を書き、偽の ffmpeg は本当に crop する。最後の mux で、concat の一覧の各絵の色がファイル名の状態と合うかを確かめて記録する
- テスト 4 本: 全状態を撮って正しく切り出し mp4 を書く / 全部同じ絵なら止める / Chrome の失敗を rc つきで報告 / mux の失敗で前回の出力を壊さない
- 変異で red: crop の位置を 0 に / 「全部同じ」の検査を外す / 切り出した絵の割り当てをずらす
