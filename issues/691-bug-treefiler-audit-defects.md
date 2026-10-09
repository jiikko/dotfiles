# 691 (bug): treefiler の不具合 (2026-10-09 の監査で実測・読解で確かめたもの)

起票日: 2026-10-09

## 概要

treefiler の監査 (697) で、実測で再現したか、コードで経路を確かめた不具合。上ほど重い。

## 詳細

1. **特定のバイト列のファイルを開くと固まる** (P1・実測) — `filer/preview.go` の `utf8Boundary` / `feed`。先頭 8 KiB に NUL が無く、
   改行の無い 16 KiB 超の連続が 0x80〜0xBF だけ (例: `A` の後に 0x80 が 2 万個) だと `utf8Boundary` が 0 を返し、`feed` が `partial[0:]` のまま
   空行を足し続ける。タイルを開く (`ensure`) と UI が固まり、メモリが増え続ける。repo に置いたファイルを開くだけで踏む。
   直し方: 切れ目が 0 なら `maxLineBytes` で強制的に切る
2. **巨大なフォルダで検索の 1 打鍵が約 10 秒固まる** (P1・実測 5 万件で 9.66 秒) — `filer/search.go` の `column` が map の順に集めた配列を
   `sortByY` (挿入ソート O(n²)) で並べる。`stepMatch` の `inTree` も一致ごとに O(n)。`n` `N` は `column` を 2 回呼ぶ。直し方: `sort.Slice` か y 順で直接集める
3. **root が `/` のときパスの組み立てが壊れる** (P2・実測) — `filer/explode.go` の `loadPath` (`m.root.abs+string(os.PathSeparator)`)・
   `filer/model.go` の `findNode`・`filer/walk.go` の `forget` が `//` を作る。`New("/")` で `/etc` の `findNode` / `loadPath` が nil。ライブ更新・`e`・
   places の復元・タイルのリンクが黙って効かない。`filer/git.go` の `withSep` だけ対策済み。直し方: `withSep` (か共通の `hasPathPrefix`) に寄せる
4. **単体の treefiler が `Close` を呼ばず、Remember place が保存されない** (P2・確認) — `main.go` に `Close()` の呼び出しが無く、`savePlace` は
   `Close` の中だけ。glogx は `hide()` で呼ぶので動く。直し方: Quit の前に `f.Close()`
5. **タイルのリンクが別のフォルダのファイルへ飛ぶ** (P2・実測) — `Model.linkCache` (`filer/tile.go` の `resolveLink`) の鍵がトークンの文字列
   だけで、解決に使う root と base (タイルのファイルのフォルダ) が入っていない。a/note.txt と b/note.txt がどちらも `util.go` と書くと、b のタイルの
   Tab → Enter が a/util.go を開く。負のキャッシュも固定され、後から作ったファイルはリンクにならない (Refresh でも捨てない)。直し方: 鍵を (base, tok) に
6. **閉じて開き直すと、閉じている間の変化が偽の光で出る** (P2・実測) — `filer/watch.go` の `close` が `seen` と `pending` を捨てない。
   glogx で F を閉じる → ファイルを変える → F で開く (Refresh で木は最新) の約 1 秒後に古い基準との差が届き、光と読み直しが走る。直し方: `close` で捨てる
7. **ライブ更新が走査中の新しい基準を消す** (P3・実測) — `filer/watch.go` の `loop` 末尾の `delete(w.seen, d)` が、周の途中に `setDirs` が入れた
   基準を消す。開いた直後の変化が報告されない (`r` で直る)。直し方: 消すのを周の開始時点の `want` との差に限る
8. **ライブ更新のたびに祖先の熱の色と大きさが消え、root まで走査し直す** (P2・実測) — `filer/walk.go` の `forget` が祖先と子孫の結果を
   消す (`applyChange` から)。毎秒変わるフォルダ (ログ) があると root からの走査が毎秒走る。spec §5.1 で invalidate するのは `r` だけ。
   直し方: 消さずに「古い」の印を付けて値を残し、新しい結果で差し替える。走査中の古い結果の書き戻し (`loop` が forget 後に無条件に書く) も世代で防ぐ
9. **`!` と `s` の起動失敗が黙る** (P2・読解) — `main.go` の `tea.ExecProcess(cmd, func(error) tea.Msg {...})` がエラーを捨てる。作業フォルダが
   消えた・`$SHELL` が実行できないと、何も出ずに戻るだけ。glogx 側 (`runEditorCmd`) の扱いは未確認。直し方: エラーを載せて toast にする
10. **`/Users` から `-` で `/` へ上がれない** (P3・実測) — `filer/model.go` の `rerootUp` が `LastIndexByte(...) > 0` で `/Users` を弾き、何も出さない
11. **保存の失敗・設定の悪い行が、板を開くまで見えない** (P3・読解) — `filer/settings.go` の `applySettings` (`.` キーなど板の外の変更の
    `saveErr`)・`setErrs`。`readSig` / `ensure` の読み込みの失敗も EOF 扱いで、タイルが途中で切れても理由が出ない
12. **`branchLabel` が空の branch で panic しうる** (未確認) — `filer/git.go` の `strings.Fields(name+" ")[0]`。git がそれを出す条件は見つかっていない

## 対応方針

1・2 を先に (固まる)。3・5・6・7・8 はそれぞれ回帰テストと変異で閉じる。

## 関連ファイル

- `src/treefiler/filer/preview.go`・`search.go`・`explode.go`・`model.go`・`walk.go`・`watch.go`・`tile.go`・`git.go`・`settings.go`、`src/treefiler/main.go`
- 監査の記録: 697

## 進捗

- [ ] 未着手
