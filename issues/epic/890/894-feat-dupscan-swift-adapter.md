# 894 (feat): dupscan の Swift adapter (tree-sitter-swift)

起票日: 2026-10-24

## 概要

[891 (要件の正本)](891-design-dupscan-requirements.md) の「adapter」節と「実測」節に沿って、Swift の adapter を作る: 文法の pin (`with-generated-files` branch)・node の種類から役割への対応表
(暗黙のメンバー・constructor_expression・macro_invocation・文字列の埋め込み・trailing closure などの落とし穴)・test / prod の既定の規則・テスト関数の見分け方・所属ターゲットの解決。

## 受け入れ条件

- [ ] 所属ターゲットを `Package.swift`・XcodeGen の `project.yml`・追跡されている `project.pbxproj` から解決する。どれも無ければ「不明」と出す。
      obaket と DualNoteApp (XcodeGen と SwiftPM。`project.pbxproj` は追跡していないがディスク上にはある) の両方で、代表のファイルの所属が正しいことを確かめる。
      追跡されていない `project.pbxproj` は読まない (手元と CI で結果が割れないように) ことをテストで固定する。`project.yml` は target の `sources` のパスだけを読み、
      `include:`・templates など読めない書き方に当たったら、その target は「不明」にする

- [ ] 網羅テスト: node-types.json の named な種類が全部、対応表で分類されている (または明示して無視している)
- [ ] 固定の入力: obaket の PR 24 の前の木 (commit 45cc3b208^) から 6 つの `location` の定義を含むファイルを切り出して Swift adapter のテストに置き
      (出典の commit とパスをテストに書く)、段ごとの期待値を書く (L1 / L2 で直書きの 4 つが 1 組、L3 で 6 つが 1 組)
- [ ] canary: 本物の parser と adapter を通して必ず 1 組出る入力 (core だけの合成の木では adapter の退行を検出しない)。読めない構文を含む入力 (除外の件数が報告に出る)

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 所属ターゲットの解決に XcodeGen の `project.yml` を足し、DualNoteApp でも確かめる条件にした (ユーザー 2026-10-24: repo に依存しない汎用的な作りにしたい。途中の検証には DualNoteApp の macOS を使ってよい)
