# 694 (refactor): treefiler の設計の課題 (697 の監査の P3 群)

> 🚨 **担当中: Claude Code (dotfiles-53。監査の issue を順に直すセッション)**（2026-10-09〜）

起票日: 2026-10-09

## 概要

どれも今は動いているが、項目を足したときに黙って片方だけ効く形。1 つの発見は一番狭い型に 1 回だけ数えた。

## 詳細

1. **設定の適用が New と applySettings に二重** (E2) — `filer/model.go` の `New` と `filer/settings.go` の `applySettings` が同じ 3 行
   (配色・`labelMax` などパッケージ変数・`showHidden`) を持つ。副作用は `key == "git"` などの文字列の分岐。直し方: New から保存なしの適用を 1 本呼ぶ。
   `m.showHidden` のコピーをやめ `m.set.ShowHidden` を読む
2. **選択肢つきの設定が string で、写しの表が手書き** (polymorphism) — items の `fixed(...)` と `speedFactor` / `lineGlyphs` / `heatRanges` / `accents` /
   `palettes`・`details()`・`glyph` の `style == "rounded"` が対。今は全選択肢に写しがある (実測) が、突き合わせるテストが無い。speed を足して写しを
   忘れると動きが止まる。直し方: 突き合わせのテスト 1 本、または型と表
3. **「見える名前か」の判定が 5 実装** (duplication。母集合: `filer/*.go` の `HasPrefix(*, ".")` の 5 箇所) — `node.hidden`・`applyChange` の `visible`・
   `removedVisible`・explode の skip・`walk.go` の hidden。`kids`/`hasKids` だけが「カーソルの経路上なら見せる」を持つので、隠した dotfile の中に
   カーソルがあるとその変化が光らない。直し方: `Model.visible(k)` に寄せる
4. **path → node の解決が 3 実装** (duplication) — `loadPath` (explode.go)・`nodeFor` (tile.go。区切りを "/" 決め打ち・孤児の node)・`findNode`
   (model.go)。`nodeFor` のコメント「木は変えない」は `ensureLoaded` で読むので不正確。直し方: `loadPath` を正本にする (691 の 3 と一緒に)
5. **入力欄の幾何を 4 箇所で手計算** (E2) — `searchBar` / `promptBar` / `CaretPos` の 2 分岐が prefix の幅と窓の幅を各自で計算。見た目を変えると
   IME のキャレットだけずれる。直し方: 入力欄の型に (表示, キャレットの x) を返させる
6. **OwnsKeys が help を含まず、glogx がキーを決め打ちで知っている** (L5 / L3) — `?` の一覧を出している間に F / i / R / D / U を押すと、一覧を
   閉じるだけのはずが glogx が閉じる・横断する (help は true のまま残る)。`OwnsKeys` の doc「今は入力欄を持たない」も古い。glogx の
   `filer_view.go` は `s` `C` `U` を filer の語彙として決め打ちで知る。直し方: help を OwnsKeys に含める・filer が予約するキーを公開する
7. **Exec の後の契約が呼び出し側に 2 実装** (L5) — Exec の後に Refresh を呼ぶ契約が glogx (`execPending`) と `main.go` (`execDoneMsg`) で別。
   ExecRequest → exec.Cmd の変換 (Dir・`append(os.Environ(), ...)`) も 2 箇所。直し方: filer が `ExecDone()` を持ち、Env を完成形で渡す
8. **Busy が非同期の源を手で 7 つ並べる** (responsibility / E2) — `Model.Busy`。源を足して入れ忘れると glogx が tick を止め、結果が次のキーまで
   取り込まれない (今は全部入っている)。分割案: 裏の処理を (busy, take) の対で持つ型へ出し、Model の recs / recsVer / gitSnap / gitVer / repoTop を
   移す (Busy の入れ忘れが構造で消える)。描画を別ファイルへ移すだけの分割は複雑性を動かすだけなので提案しない
9. **git の状態が生の byte** (polymorphism) — `gitRank`・`gitWord`・`classifyXY`・`diffable`・`drawNames` (`string(st)` を記号にも使う)・`gitColor`
   (ignored が無い)。状態を 1 つ足すと 5〜6 箇所を手で直す。直し方: {記号, 重さ, 語, diff できるか} の表
10. **枠の描画が 3 箇所** (ui-components) — `drawHelp`・`drawPanel`・`drawTile`。tuikit の layout は文字列の部品で canvas (セル + rgb) に合わない。
    直し方: canvas に `frame(rect, title, col)` を足す程度
11. **死んだ分岐と未使用** (dead-code) — `treeKey` の `"ctrl+c"` と `panelKey` の `"ctrl+c"` は `HandleInput` が先に Quit を返すので到達しない
    (変異で外しても全テスト green)。`SearchQuery` は production から呼ばれずテストだけ (glogx のテストを含む)。spec の板の Ctrl-C (閉じる) と実装
    (glogx ごと終了) の食い違いは §0 の決定が優先されたか未確認

## 関連

- 監査の記録: 697。3・4 は 691 の 3 (root が `/`) と同じ所を直す

## 進捗

- [ ] 未着手
