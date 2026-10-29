# 974 (bug): L1 が局所の名前と同じ綴りの外側の名前も番号にし、違う package・プロパティを使う 2 つを同じ形にする

起票日: 2026-10-29

概要: norm の局所の名前の表が scope を見ず綴りで照合するので、`bytes := bytes.TrimSpace(b)` と `strings := strings.TrimSpace(s)`、Swift の `if let user` と `if let account` が L1 の組になる。

> 反証レビュー: 2026-10-30 に通した (opus。結果は下の「結果」)。

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

- [x] 再現 1・2 をテストにして赤を確かめてから直す (変異で固定)
- [x] hash が変わる単位の数を dotfiles の report の前後で数えて書く (下の「結果」。Go 2 repo・Swift 2 repo)
- [x] 963 の L1 の行の断り書きを、直した範囲に合わせて見直す (直らない形が残るので、残る形だけを言う文にした)

## 関連

- [963](963-research-dupscan-feedback-obaket-1309-2026-10-29.md): L1 の行の文を弱めた。反証レビュー 2 周目で見つかった

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

## 結果 (2026-10-30、dotfiles-31)

commit「fix(dupscan): L1 で、局所の名前と同じ綴りの外側の名前を番号にしない (issue 974)」。

- 反証レビュー (着手前): 再現 2 は起きる。ただし当初の方針 1・2 では足りない。Go の同じ文の右辺だけに印を付ける案は、range・関数リテラルの引数・
  後で宣言する綴り・別の block の宣言・兄弟の block の 5 形が直らない。Swift の shorthand だけの扱いでは、`if let x = x`・`let x = x…` も直らない
- 採った形:
  - `tree.Node.Outer` (単位の外の名前を指す参照) を足し、norm の `isLocal` が Outer を局所としない
  - Go は parser の名前解決 (`SkipObjectResolution` を外した) で、宣言が単位の範囲の外か未解決の名前を Outer にする。上の 5 形も直る
  - Swift は名前解決が無いので近似 (`internal/lang/swift/outer.go` の `markOuter`): 引数と、その位置までに効いた宣言だけを局所と読む。
    宣言は `=` / for の `in` の右辺の後で効く (型注釈・case パターンの括弧を越えて探し、深さ 0 の `,` で止める)。
    shorthand の `if let x` は宣言の子に x の参照を持ち、引数なら番号・プロパティなら字面
- 残る近似 (Swift): block・closure・ループの中や if / guard の条件で宣言した綴りを、束縛の届かない所 (その外・else の中・if の後ろ) で使う所は、
  局所と読む (誤検知の向き)。`-v` の L1 の行と README の「分かっている限界」に書いた
- 前後の比較 (組の集合。変更前の版と同じ入力で):
  | repo | 組 (前 → 後) | L1 の hash の変わる単位 |
  |---|---|---|
  | dotfiles `src` (Go) | 55 → 55 | 6 / 8976 (訂正。下の注) |
  | pj_energy_matching (Go) | 41 → 41 | 12 / 2376 (訂正。下の注) |
  | flickorder-client (Swift) | 183 → 183 | 797 / 7777 |
  | DualNoteApp (Swift) | 104 → 104 | 167 / 2225 |
  - 訂正 (2026-10-30、990 の測定で気づいた): Go の数は、最初に「dotfiles 55 / 8967・pj 14 / 2388」と書いた。単位の鍵 (複数行の署名) の
    改行とタブで、行区切りの TSV の突き合わせが鍵と hash を取り違えていた。鍵を引用して数え直すと、974 の前後の commit で dotfiles 6 / 8976・
    pj 12 / 2376。Swift の数 (797・167) は鍵に改行が無く、正しい。commit のメッセージの数は前のまま
  - Swift の hash の変化は、shorthand に参照の node を足した分が大半 (flickorder の shorthand は約 486 か所。closure と囲む関数の両方が変わる)
  - 実装の途中で、shorthand をいつも外側と読む版は flickorder の正しい組を 2 つ落とした (引数を読む `if let since`)。引数なら番号にする形に直した
  - 測った repo には `.dupscan.json` が無く、釘の外れは数えていない。README の除外の節に、正規化を変えたときの釘の付け直し方を書いた
  - 所要: どの repo も変わらず (Go の parse は名前解決の分で user 時間が約 +12%。red team の測定)
- テスト: `src/dupscan/l1_shadow_test.go` (Go 3・Swift 10 の形と対照)・`internal/lang/swift/outer_test.go`。変異 13 本で red
- 敵対的レビュー 2 周 (opus): 1 周目は型注釈・case パターンの束縛と、断り書きの範囲の狭さ。2 周目は case パターンの括弧の中の `,`。どれも直してテストを足した。
  3 周目は回していない (括弧の深さの修正は、指摘の入力をテストにして変異で red を確かめただけ)
