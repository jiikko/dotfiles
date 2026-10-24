# 894 (feat): dupscan の Swift adapter (tree-sitter-swift)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-24〜）

起票日: 2026-10-24

## 概要

[891 (要件の正本)](891-design-dupscan-requirements.md) の「adapter」節と「実測」節に沿って、Swift の adapter を作る: 文法の pin (`with-generated-files` branch)・node の種類から役割への対応表
(暗黙のメンバー・constructor_expression・macro_invocation・文字列の埋め込み・trailing closure などの落とし穴)・test / prod の既定の規則・テスト関数の見分け方・所属ターゲットの解決。

## 受け入れ条件

- [x] テスト関数を、XCTest (XCTestCase のサブクラスの `func test…`) と Swift Testing (`@Test`) の両方で見分ける。DualNoteApp は XCTest だけ (60 ファイル)、obaket は Swift Testing なので、両方の repo で確かめる
- [x] DualNoteApp (Swift 6.0) の全ファイルで、読めない構文があるファイルの数を測る (obaket の 3% と並べる)

- [ ] 所属ターゲットを `Package.swift`・XcodeGen の `project.yml`・追跡されている `project.pbxproj` から解決する。どれも無ければ「不明」と出す。
      obaket と DualNoteApp (XcodeGen と SwiftPM。`project.pbxproj` は追跡していないがディスク上にはある) の両方で、代表のファイルの所属が正しいことを確かめる。
      追跡されていない `project.pbxproj` は読まない (手元と CI で結果が割れないように) ことをテストで固定する。`project.yml` は target の `sources` のパスだけを読み、
      `include:`・templates など読めない書き方に当たったら、その target は「不明」にする

- [x] 網羅テスト: node-types.json の named な種類が全部、対応表で分類されている (または明示して無視している)
- [ ] 固定の入力: obaket の PR 24 の前の木 (commit 45cc3b208^) から 6 つの `location` の定義を含むファイルを切り出して Swift adapter のテストに置き
      (出典の commit とパスをテストに書く)、段ごとの期待値を書く (L1 / L2 で直書きの 4 つが 1 組、L3 で 6 つが 1 組)
- [ ] canary: 本物の parser と adapter を通して必ず 1 組出る入力 (core だけの合成の木では adapter の退行を検出しない)。読めない構文を含む入力 (除外の件数が報告に出る)

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 所属ターゲットの解決に XcodeGen の `project.yml` を足し、DualNoteApp でも確かめる条件にした (ユーザー 2026-10-24: repo に依存しない汎用的な作りにしたい。途中の検証には DualNoteApp の macOS を使ってよい)
- 2026-10-24 XCTest と Swift Testing の両方のテスト関数の見分けと、DualNoteApp での読めない構文の実測を受け入れ条件に足した (ユーザーの問い 2026-10-24: DualNoteApp と obaket は使っている道具が同じか、検証環境にする障壁は)
- 2026-10-24 Swift adapter を作った (commit「feat(dupscan): …第 1 段」): 文法の名前つきの種類 188 を式・リテラル・名前・型・コメント・構造に分ける表と、文法と突き合わせる網羅テスト
  (`bracket_qualified_type` の漏れを拾った)・単位 (関数・初期化子・クロージャ)・鍵 (ファイル + 囲む型 + 署名、クロージャは囲む単位 + 順番)・XCTest と Swift Testing のテスト関数の除外・test のパスの判定
  - canary: `docs/dupscan-detection.md` の例を本物の parser に通し、L1 / L2 で違い L3 で同じ・呼ぶ型が違えば L3 でも違うことを固定
  - DualNoteApp (Swift 6.0) の 199 ファイルで読めない構文 0 件 (obaket は 3%)
  - **残り**: PR 24 の固定の入力 (obaket がこのマシンに無い)・所属ターゲットの解決 (`Package.swift`・`project.yml`)
- 2026-10-24 敵対的レビューを受けて直した: 単位に computed property・subscript・deinit・`didSet` / `willSet` を足した・鍵に `static`・extension の `where`・入れ子の関数の親を入れ、重なれば番号を付ける・子の無い節 (`<+>`・`outer:`) の字面と `#if` をハッシュに入れる・`@Testing.Test` と `XCTestCase` を継承した型の中の `test…` を見分ける・読めない構文に接した単位 (入れ物に着くまでの祖先が ERROR を含む) を比べない
