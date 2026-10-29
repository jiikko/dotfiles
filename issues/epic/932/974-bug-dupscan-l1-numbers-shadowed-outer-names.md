# 974 (bug): L1 が局所の名前と同じ綴りの外側の名前も番号にし、違う package・プロパティを使う 2 つを同じ形にする

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

起票日: 2026-10-29

概要: norm の局所の名前の表が scope を見ず綴りで照合するので、`bytes := bytes.TrimSpace(b)` と `strings := strings.TrimSpace(s)`、Swift の `if let user` と `if let account` が L1 の組になる。

> 反証レビュー: **未実施** (5h 枠の残りで本文までを書いた。着手の前に通す)。

## 詳細

963 の反証レビュー 2 周目で見つかり、手元で再現した (963 の `-v` の L1 の行の文は、これを受けて「外側の同じ綴りの名前も番号になる」と断る形にした。直すのはこの issue)。

- 原因: `internal/norm/norm.go` の `localNames` は、本体の中の宣言 (`RoleDecl`) の綴りを単位全体の表に入れ、`isLocal` は同じ綴りの参照 (`RoleName`) を位置を問わず局所の名前として番号にする。scope と宣言の前後を見ない
- 再現 1 (Go): `bytes := bytes.TrimSpace(b)` と `strings := strings.TrimSpace(s)` の 2 つの関数 (ほかは同じ) が L1 の組になる。Go では右辺の `bytes` は文の後で効く宣言より前で、package を指す
- 再現 2 (Swift): `if let user { show(user) … }` と `if let account { … }` (別のプロパティを読む 2 つのクラスの `render()`) が L1 の組になる。shorthand の束縛は「新しい局所の名前」と「外側のプロパティの参照」を兼ねるが、番号にしかならない
- 影響: L1 の組は「本体の名前とリテラルが同じ」と読まれる。`explain -compare` も L1 の構造で比べるので差 0 を返し、別物を同じとする方へ誘導する (963 の目的の逆)

## 対応方針 (候補。反証レビューの後に決める)

1. Go: 短い変数宣言・`var x = …` の右辺に出る同じ綴りの名前は、その文の宣言より前の参照として外側の名前のまま残す (adapter が右辺の参照に「宣言の前」の印を付けるか、norm が文の中で宣言の効く位置を持つ)
2. Swift: shorthand の束縛 (`if let x` / `guard let x`) は番号に加えて外側の名前を形に残す (`x = <外側の x>` と同じ形にする)。`isOptionalBindingShorthand` の扱いを norm に伝える
3. どちらも L1 の hash が変わる単位がある。使う側の `.dupscan.json` の釘 (`@<L1 の hash>`) が効かなくなるので、変わる単位の数を dotfiles と手元で取れる repo で数え、CHANGELOG 相当 (README) に書く。数が多ければ、旧 hash の釘を移行する手 (`-v` の「詳しく:」で記録と今の hash を出す 961 の仕組み) で足りるかを確かめる

## 受け入れ条件

- [ ] 再現 1・2 をテストにして赤を確かめてから直す (変異で固定)
- [ ] hash が変わる単位の数を dotfiles の report の前後で数えて書く
- [ ] 963 の L1 の行の断り書きを、直した範囲に合わせて見直す (直らない形が残るなら残す)

## 関連

- [963](done/963-research-dupscan-feedback-obaket-1309-2026-10-29.md): L1 の行の文を弱めた。反証レビュー 2 周目で見つかった

## 着手の記録 (2026-10-29 夜、dotfiles-31。5h 枠の上限で中断・claim は外した)

- 再現 1 を本物の `bin/dupscan -v` で確認: `bytes := bytes.TrimSpace(b)` と `strings := strings.TrimSpace(s)` の 2 関数が L1 の組になる。
  対照 (局所の変数を `t` にした同じ 2 関数) は「重複なし」。原因は外側と同じ綴りの局所の名前で、issue の記述どおり。再現 2 (Swift) は未確認
- 反証レビューは未実施のまま
- 直し方の当たり (Go): 宣言は `internal/lang/golang/conv.go` の `walker.decl` が `RoleDecl` にし、`AssignStmt` (`:=`) は左辺の宣言を右辺より先に子に並べる。
  norm (`internal/norm/norm.go` の `localNames` / `isLocal`) は綴りだけで照合するので、木の順で「宣言より後」と判定しても右辺は救えない。
  adapter の側で、`:=` と `ValueSpec` の右辺に出る「その文で宣言する名前と同じ綴り」の参照のうち、引数 (`Unit.Params`) と
  それより前の宣言に無いものへ印 (例: `tree.Node` に外側を指す印) を付け、norm がその参照を番号にせず字面で残す案。
  `x := x + 1` (引数 x の更新) を壊さないため、既出の名前は番号のままにする。walker は今は既出の宣言を持っていない
- まだコードは書いていない (worktree の変更なし)
