# 734 (refactor): treefiler の設計の課題 (2026-10-12 の監査)

> 🚨 **担当中: dotfiles-4e**（2026-10-12〜）

起票日: 2026-10-12

## 概要

監査 (737) の設計系の発見。どれも今は動いているが、次に操作・層・呼び出し側を足すときに壊れやすい形。上ほど優先。

## 詳細

1. **(E2) 「人が決めた時点の実体 (dev, ino)」を 2 つの構造体が別々に持つ** — `filer/fileops.go` の `trashAsk{n, dev, ino}` と `promptState{target, dev, ino}`、
   `startRename` / `startTrash` の同じ前段 (root か → `identityOf` → 失敗を fail)、`safefs.Rename` / `TrashMove` の `dev, ino uint64` の 2 引数 (取り違えても型が通り、
   ErrSwapped に倒れて「項目が変わりました」と出る)。3 つ目の「確かめてから実行する」操作 (移動・複製) を足すと 3 か所目になる。直し方: `safefs.ID{Dev, Ino}` と
   `pinned{n, id}` に寄せる
2. **(duplication) 経路に symlink が無いかの判定が 2 つ** — `filer/fileops.go` の `safeDir` (root だけ辿り、そこから 1 段ずつ) と `safefs.NoSymlinkInPath` (全段。/var 等を畳む)。
   意味が違うので 1 つにはならないが、「root の中の相対の経路を 1 段ずつ辿って symlink を断る」は safefs に置ける (`safefs.ResolveUnder(root, abs)`)。3 つ目の呼び出し側が
   書き直すと 3 実装になる
3. **(E2) glogx の `filerView` の「shown なら f は nil でない」を各メソッドが確かめ直している** — `glogx/filer_view.go` の `v.shown && v.f != nil` (約 9 メソッド)・
   `filer_resume.go`・`tui.go`。`handleKey` / `lines` は確かめずに使う。`toggle` 以外で shown を立てる経路を足すと挙動が割れる。直し方: `live()` 1 本に
4. 記録 (直さない): `virt.go` の `resetVirt` と `layout.go` で「巨大か」の入力が違う (生の件数 / 見せる子の件数。今は害なし) /
   大きさ・経過時間の整形 (`human` / `ago` 等) の別実装が 4 module にある (単位と丸めが同じかは未確認。716 の領分)

## 関連

- 737 (この監査)・694 (前回の設計の課題)・722 (ファイル操作)

## 進捗

- 2026-10-12: 起票 (監査 737)
