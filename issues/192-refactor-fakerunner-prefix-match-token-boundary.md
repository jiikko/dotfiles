# 192 refactor: `fakeRunner` の応答選択がトークン境界を見ておらず、将来の fixture で誤マッチする

起票日: 2026-09-03
重要度: **P3**
関連: [issues/done/168](done/168-bug-doctor-simdevice-xctestdevices-set.md) (敵対レビュー 1 周目の記録)

## 対象

`src/doctor/disk/scan_test.go` の `fakeRunner.run` (応答キーの選択)

## 何が起きるか

`fakeRunner` は「コマンド行に対する **最長の `strings.HasPrefix` 一致**」で応答を選ぶ。
トークン境界を見ていないので、**あるキーの全体が別のコマンド行の literal な prefix になっている**と、
短い方のキーが長い方の呼び出しを横取りする。

現状は誤マッチしない (2026-09-03 の敵対レビューが確認):

- `xcrun simctl --set <dir> list devices -j` に渡す 3 つのパス
  (`~/Library/Developer/XCTestDevices` / `XCPGDevices` / `.../Previews/Simulator Devices`) は
  互いに prefix 関係に無い
- 他のキー (`pgrep -x` / `brew info` / `brew --prefix` / `brew cleanup --dry-run`) も同様

**将来 `.../FooDevices` と `.../FooDevicesExtra` のような prefix 関係にある fixture を足すと壊れる。**
壊れ方は「テストが別の応答を拾って green のまま意味を失う」なので、気づきにくい。

## 対応案

- キーの一致を「トークン境界まで見る」形にする (キーの直後が空白か行末であることを要求する)
- または fixture を `[]string` の argv で持ち、`slices.Equal` / prefix を argv 単位で比べる

## 受け入れ条件

- [ ] prefix 関係にある 2 つのキーを登録したテストで、長い方の呼び出しが長い方の応答を拾う
- [ ] 変異検証: トークン境界の判定を外すと誤マッチが再現する

## 事前検証

未着手 (2026-09-03 時点で誤マッチする fixture は存在しない = **予防的な refactor**)。
[`verify-design-intent-before-refactor.md`](../_claude/rules/verify-design-intent-before-refactor.md) の
「実需要 trigger 待ち」に従うなら、**次に prefix 関係のある fixture を足したくなったとき**が着手の trigger。
