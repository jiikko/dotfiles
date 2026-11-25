# swiftui-observe-collection-per-element の根拠

## 起源

obaket で同じ形が 3 回起きた (いずれも shared/Sources/ObaketPresentation)。

- issue 917: 行ごとの転送の進捗。`BrowserRowTransferProgressStore` は `entries` 辞書を観測させるが、代入はキーの集合が変わったときだけにし、
  値は行ごとの `Entry` に持つ (ルールの 1 つ目の形)
- issue 1351: 選択の集合 `selectedIdentities` を全カードが読み、カーソル移動のたびに全カード (30 枚前後) を評価し直していた。
  identity ごとの `Membership` (`@Observable`) に分け、`selectedIdentities` の didSet だけが写す (2 つ目の形 + 書き込み口を 1 つ)。
  一覧から消えた identity の `Membership` は書かずに捨てる (`pruneMemberships`。読むカードが同じ更新で消える)
- issue 1582 (2026-11-25。実装は obaket の commit「perf(1582): サムネの store を key ごとの観測にし…」、issue は進行中): サムネの辞書 `PreviewThumbnailStore.images` を全カードが読み、1 枚の store で grid のカード 5 枚中 5 枚を評価し直していた。
  key ごとの `Slot` に分けた。view の読み取りで空の枠を作るので、空の枠に別の上限を置いた。外すときは `.removed` を書く。
  全カードの読み直しが LRU の recency を更新していたので、見えているサムネを追い出しから外す経路を別に足した

同じ規範は ThumbnailThumb の `.claude/rules/swift-coding.md` にも「View body 内で粗粒度プロパティを直接読み取り → 不要な View 再描画が連鎖」として
書かれ始めていた (repo ごとに書き始める前に global へ置いた)。

## 通知を省く条件

- 実測: obaket issue 1218 で、Equatable の配列を同じ値で代入しても通知が出ないことを変異で確かめた (Swift 6.4)
- 推測 (実行して確かめていない): Swift 6.4.2 の `libObservationMacros.dylib` の文字列に、マクロが展開する `shouldNotifyObservers` の多重定義が
  複数ある (`Equatable & AnyObject` に `lhs != rhs`、`AnyObject` に `lhs !== rhs` などを読めた。断片しか取れていない)。ここから、参照型の同じ instance の
  書き込みと、class かつ Equatable の型で `==` の別 instance の書き込みも、通知が省かれると読んでいる

`Observation` の swiftinterface はマクロの宣言しか
持たないので、挙動の根拠にはならない。toolchain が変わったら測り直す。
