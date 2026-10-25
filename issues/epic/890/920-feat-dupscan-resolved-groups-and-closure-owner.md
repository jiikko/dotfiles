# 920 (feat): dupscan: 寄せて消えた組を gate で出し、クロージャに囲む関数の名前を出す

> 🚨 **担当中: M4Pro-MacBook-Pro (glogx)**（2026-10-25〜）

起票日: 2026-10-25

## 概要

obaket の 1306 (dupscan が出した類似メソッド 8 組を寄せる作業。2026-10-25) で dupscan を使って見えた使いにくい点 2 つを直す。
ユーザーの依頼 (2026-10-25):「使いにくい点は dotfiles の issues md として書いといて。余裕があればそのまま実装して」。

1. **寄せた結果を確かめる手段が無い**。寄せる作業の完了条件は「組が消えた」だが、gate が出すのは増えた組 (違反) だけで、消えた組は出さない。
   1306 では統合の前後で `dupscan report -format text -limit 0` を 2 回取り、メンバーの行を「ファイル + 名前」に直して組の集合を
   突き合わせる Python を手で書いた (行番号は寄せると動くので、report の行をそのまま比べられない)
2. **クロージャの名前が `型.{closure}` だけで、どの関数の中か分からない**。1306 の表には
   `ManualTransferPacingClock.{closure}` が 2 行並び (`advanceToNextDeadline` の中の `min(by:)` と `sorted` の比較)、
   `StorageByteStream.{closure}` も 2 行並んだ (2 つの init の既定引数)。行番号を開かないと区別できない

## 詳細

### 1. 消えた組

- gate は base と head の単位を既に両方読み、head の単位ごとに base の側の相手 (出どころ) を当てている (`internal/gate/gate.go` の `origins`)。
  消えた組は、この対応と、base・head それぞれの組 (`group.Build`) から出せる。新しく読むものは無い
- 定義: base の組 (report と同じ `group.Build` の組。除外は掛けない) のメンバーについて、head の側の相手を引く。相手のうち 2 つ以上が、
  見ている段のどれかで同じ形なら「残っている」。どこにも 2 つ以上揃わなければ「消えた」。揃った最大の数がメンバーの数より少なければ「縮んだ」
  - head の側は段ごとの生のハッシュで見る (`group.Build` を通さない)。通すと、外側の関数を同じ組にした (悪化させた) とき、
    中のクロージャの組が入れ子 (dropNested) として外れ、消えたと数える (反証レビューの指摘)
  - 相手が読めない構文になった組は数えない (比べられなくなっただけ。gate は同じ単位を `unreadable_changed_units` に分けている)
  - 除外を掛けない: 除外は組を消さず、出さないだけ。除外を掛けた組で比べると「除外を足した」が「寄せた」に数えられる。
    また `exclude.Set` の判定 (`GroupExcluded` 等) は使われた除外を数える副作用を持つので、base の組に掛けると効いていない除外の数が狂う
  - 段を問わない: L1 の組の 1 つが定数だけ変わって L2 の組で残るのは、寄せていない
- 出し方: gate の要約の行に「消えた組 N・縮んだ組 M」を足し、`-v` で 1 組ずつ (base の側の位置と名前、メンバーの数 → 残った数)。
  JSON (`-format json`) には `resolved` として常に出す。`-record` の記録にも同じ値が載る

### 2. クロージャの名前

- swift の adapter (`internal/lang/swift/swift.go` の `closureUnit`) は、囲む単位の鍵 (`ctx.owner`) は持つが名前を持たない
- 囲む単位の名前を単位に持たせ (`tree.Unit` に表示用の欄を足す)、表示 (`internal/report/report.go` の `label`) を
  `型.advanceToNextDeadline.{closure}` にする。入れ子のクロージャは `型.f.{closure}.{closure}`
- **鍵と `Name` は変えない**。鍵は `.dupscan.json` の除外の釘 (`key@hash`) と gate の出どころの当て方に使われ、`Name` は出どころの当て方
  (同じファイルの同じ名前を先に当てる) と index に使われる。変えると既にある除外が外れ、当て方の順が変わる。表示だけを変える

## 受け入れ条件

- [x] gate が消えた組・縮んだ組を数え、`-v` で一覧を出す。JSON に `resolved` が載る
- [x] 消えた組を数えるテスト: 寄せた (メンバーを消した・本体を変えた)・除外を足しただけ (消えない)・定数だけ変えた (消えない)・3 → 2 (縮んだ)
- [x] クロージャの表示に囲む関数の名前が出る。鍵と `Name` が変わらないことをテストで固定する
- [x] 足したテストに変異を当てて赤を見る
- [x] README の gate と出力の節を直す

## 関連ファイル

- `src/dupscan/internal/gate/gate.go` (`Judge` / `origins`)
- `src/dupscan/internal/report/gate.go` (`GateSummary` / `GateText`)
- `src/dupscan/internal/report/report.go` (`UnitRef` / `label`)
- `src/dupscan/internal/lang/swift/swift.go` (`ctx` / `closureUnit`)
- `src/dupscan/internal/tree/tree.go` (`Unit`)

## 進捗

- 2026-10-25 起票
- 2026-10-25 反証レビュー (sonnet。codex の 5h 枠が超過していたため代替) の結果を本文へ反映: 読めない構文の相手を数えない・head の側を
  段ごとの生のハッシュで見る (dropNested の非対称) を採った。採らずに仕様として README に書いたもの: 相手が下限より小さくなった組は消えたと数える
  (1 行の委譲に縮めるのは寄せる作業の普通の形。obaket 1306 の debug の pacer 3 組がこの形)・写しを同じ形の薄い委譲に置き換えて下限以上に残ると
  消えない (report にも組として残るので一致している)・起点を絞った実行で起点の外へ移した単位は消えたと数える。
  クロージャの表示は、同じ関数の中の 2 つのクロージャ (1306 の `advanceToNextDeadline` の `min(by:)` と `sorted`) は同じ表示のまま
  (番号は鍵の出現順で前に足すとずれるので出さない。行番号で区別する)
- 2026-10-25 実装 (commit「feat(dupscan): gate で寄せて消えた組を出し、クロージャに囲む単位の名前を出す (issue 920)」)
  - obaket の 1306 (base `3b19561e5` → head `b25079dd1`) で `gate -v`: 消えた組 8・縮んだ組 0。手で突き合わせた 8 組と一致。
    クロージャは `ManualTransferPacingClock.advanceToNextDeadline.{closure}`・`FiniteTransferLedger.validateDisjoint.{closure}` /
    `coversExactly.{closure}`・`StorageByteStream.init.{closure}` のように出た
  - `make -C src/dupscan lint` (0 issues)・`make -C src/dupscan test` rc=0
  - 変異 (mutate-verify、7 本とも想定のテストが赤): 読めない相手の除外を外す・head を group.Build で数える・揃った数 1 を 0 に丸めない・
    見る段を先頭の 1 つに絞る → `TestResolvedGroups` / `TestResolvedSkipsUnreadableAndIgnoresNestedDrop`。
    OwnerName を入れない → `TestClosureOwnerNameIsForDisplayOnly`。label が OwnerName を使わない → `TestClosureLabelShowsOwner`。
    要約の行の数を出さない → `TestGateListsResolvedGroupAfterRemovingCopy`
