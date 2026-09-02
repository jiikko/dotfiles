# 169 bug: `xctest-logarchive` の glob が「実測」と書かれているが実測の記録が無く、黙って 0 件になりうる

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 1) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「敵対的レビュー 2 回目」の xctest-logarchive P2)

## 対象

`src/doctor/disk/catalog.go` の `xctest-logarchive` エントリの Paths と、その直近コメント
(「実測の名前: `XCTestTesting.<uuid>.logarchive` / `xctest-*.logarchive`」)

## 何が起きるか

前回の敵対的レビューで、glob `/private/var/tmp/*.logarchive` が「人が `log collect` で採った証跡」にも当たる P2 を
`XCTest*` / `xctest*` に絞って直した。しかし**絞った後の名前が実測されていない**。

裏取りの結果 (体 1):

- issue 148 の記録は `/var/tmp/*.logarchive` が 1.8GB あったという**サイズだけ**で、ファイル名が残っていない
- 修正 commit (824b863) の diff にも測定記録が無い
- Xcode 26 の全 framework / PlugIns / usr/bin を `grep -a` しても `XCTestTesting` を含むバイナリは **0 件**
  (spindump 側は `%@-Spindump.txt` / `Hang (Spindump).txt` が XCTHarness に在る)
- `mdfind` と Trash にも現物が無い

名前が違えば**この検出項目は黙って 0 件**になる (false negative)。P2 の修正が効いているかを誰も確認できない状態。

## 対応案

1. コメントの「実測の名前」を「**未実測**」に直す (今そこに書かれているのは推定であって実測ではない)
2. 次に `xcodebuild test` を回した直後に `ls -la /private/var/tmp/*.logarchive` で実名を採り、glob を確定する
3. 確定するまでは、検出 0 件を「候補なし」と表示するのではなく、この項目だけ「未検証の検出項目」と分かる形にする案も検討する
   (`sinking silently の禁止` = issue 148 2 章の規律)

## 再開の trigger

`xcodebuild test` を回す作業が入ったとき (iOS/macOS アプリ側の作業)。回した直後に上記 2 を実行する。

## 受け入れ条件

- [ ] コメントの主張が実測の有無と一致している
- [ ] 実名を採った後、glob がその名前に当たることを fixture テストで固定する
