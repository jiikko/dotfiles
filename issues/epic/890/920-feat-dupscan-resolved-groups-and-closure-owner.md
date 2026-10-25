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
- 定義: base の組 (除外を掛けない素の組) のメンバーについて、head の側の相手を引く。相手のうち 2 つ以上が head の同じ組 (段は問わない) に
  入っていれば「残っている」。どの head の組にも 2 つ以上揃わなければ「消えた」。揃った最大の数がメンバーの数より少なければ「縮んだ」
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

- [ ] gate が消えた組・縮んだ組を数え、`-v` で一覧を出す。JSON に `resolved` が載る
- [ ] 消えた組を数えるテスト: 寄せた (メンバーを消した・本体を変えた)・除外を足しただけ (消えない)・定数だけ変えた (消えない)・3 → 2 (縮んだ)
- [ ] クロージャの表示に囲む関数の名前が出る。鍵と `Name` が変わらないことをテストで固定する
- [ ] 足したテストに変異を当てて赤を見る
- [ ] README の gate と出力の節を直す

## 関連ファイル

- `src/dupscan/internal/gate/gate.go` (`Judge` / `origins`)
- `src/dupscan/internal/report/gate.go` (`GateSummary` / `GateText`)
- `src/dupscan/internal/report/report.go` (`UnitRef` / `label`)
- `src/dupscan/internal/lang/swift/swift.go` (`ctx` / `closureUnit`)
- `src/dupscan/internal/tree/tree.go` (`Unit`)

## 進捗

- 2026-10-25 起票
