# 731 (bug): treefiler の不具合 (2026-10-12 の監査で実測・読解で確かめたもの)

> 🚨 **担当中: dotfiles-4e**（2026-10-12〜）

起票日: 2026-10-12

## 概要

treefiler の監査 (737) で、実測で再現したか、コードで経路を確かめた不具合。上ほど重い。

## 詳細

1. **全角の文字と枠が重なると、その行が 1 桁ずれる** (P1・実測) — `filer/render.go` の `canvas.putCluster` (と `clear` / `frame`)。全角の左半分の位置へ
   半角 (タイルの右枠の `│` 等) を書いても、右半分の `cont` のセルを直さない。`lines()` は `cont` を飛ばすので、その行は w-1 桁になり、右側の内容が 1 桁左へ寄る。
   逆に右半分だけを上書きすると、左の全角が残って w+1 桁になりうる (コード上。390 通りでは未再現)。調査体の実測: 日本語名のファイルでタイルを開き、
   w=30〜159 × h={20,24,31} の 390 通りで短い行が 48 本 (`│し ` のように全角が割れる)。glogx の F も同じ canvas。
   テストが捕まえない理由: `render_ref_test.go` は旧実装と比べるだけ (旧実装も同じ盲点)・`TestViewMatchesReferenceOnRealFrames` の fixture は ASCII だけ・
   「全行の表示幅 == w」の不変条件の検査が無い。直し方: 上書きするセルが全角の片割れなら相方を空白にする (putCluster と clear) + 日本語の fixture で全行の幅を見る
2. **裏で作った知らせが、次のキーを押すまで出ない** (P2・実測) — `filer/model.go` の `Advance` の中で積む Notice (`explode.go` `takeExplode` の「開いた: N フォルダ」・
   `takeBackground` の `o` / `O` の失敗「開けません」) を、両ホスト (`treefiler/main.go` の KeyPressMsg の枝・`glogx/filer_view.go` の `routeKeyToFiler`) がキーの後にしか
   取り出さない (`TakeNotices` の production の呼び出しはこの 2 か所だけ。main が grep で確認)。`o` が失敗しても何も出ず、無関係な次のキーで古い toast が出る。
   直し方: 取り出しを 1 本にする (Advance の後にも取り出す / Advance の戻り値に含める)。両ホストを同時に直す
3. **`!` の「[done] 何かキーを押すと」で矢印キーを押すと、残りのバイトがキーとして届く** (P2・実測) — `filer/shell.go` の `waitIfQuick` の
   `dd bs=1 count=1` は 1 バイトしか読まない。← (`ESC [ D`) の `[D` が treefiler / glogx に漏れ、glogx の F では `D` が doctor 画面への横断キー、→ の `C` が
   collapseOthers になる。直し方: 1 バイト読んだ後に `stty min 0 time 1` で残りを捨てる (pty で確かめる。`verify-interactive-prompt-with-pty-driver.md`)
4. **ゴミ箱の周りの食い違い** (P3・safefs を go run して実測) — (a) `safefs/trash.go` の `TrashMove` の連番が `foo.txt 2` (Finder は `foo 2.txt`。拡張子が
   変わるので別のアプリで開かれる。doctor も同じ) (b) 255 バイトの名前が同名で衝突すると `name 2` が上限を超え、`file name too long` が素のまま toast に出る
   (`fileops.go` の `opErr`) (c) ゴミ箱の中の項目 (root を `~/.Trash`) を `d` すると、`RENAME_EXCL` が同じパスで err=nil を返し、何も動かないのに
   「ゴミ箱へ移しました」と出る。直し方: (c) は移動元がゴミ箱の直下なら断る・(a) は拡張子の前に番号
5. **`?` のキー一覧が 26 行未満の端末で末尾を黙って切る** (P3・読解のみ・画面は未確認) — `filer/model.go` の `drawHelp` (`h := min(len(helpKeys)+4, m.h-1)`。
   helpKeys は 22 行)。24 行の端末で `!` `s` `?` の 3 行が出ず、切れた印も無い
6. **`!` の履歴の追記が symlink を辿り、先を chmod する** (P3・実測) — `filer/history.go` の `appendHistory` (`os.OpenFile` + `f.Chmod(0o600)`)。
   `treefiler-history` が別のファイルへの symlink だと、先へ追記し mode を 0600 にした。読む側 (`loadHistory`) は symlink を辿らない設計で非対称。直し方: O_NOFOLLOW
7. **diff の取得の失敗を全部「HEAD が無い」と読む** (P3・未確認: 読解のみ) — `filer/diff.go` の `fetchDiff`。HEAD が在るのに `git diff HEAD` が timeout・破損で
   失敗すると `--cached` に落ち、未 stage の変更が隠れる。直し方: unborn かを `rev-parse --verify HEAD` で見てから落とす

## 関連

- 737 (この監査の記録)・732 (git の filter。repo 全体)・691 (前回の監査の不具合)

## 進捗

- 2026-10-12: 起票 (監査 737)
