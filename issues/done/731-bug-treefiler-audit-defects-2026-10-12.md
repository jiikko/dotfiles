# 731 (bug): treefiler の不具合 (2026-10-12 の監査で実測・読解で確かめたもの)

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
- 2026-10-12 (dotfiles-4e): 1〜6 を直した。commit「fix(treefiler): 監査の不具合 731 の 1〜6・性能 733・設計 734・テスト 735 を直す」
  - 1: `render.go` の `unpair` (上書きする前に全角の相方を空白にする。putCluster と clear から。clear は範囲を画面の中に切り詰めてから端だけ)。
    `widthrow_test.go` (片割れの上書き・範囲の外の clear・日本語の名前が並ぶ実際の画面を 19 幅 × 3 高さで全行の幅 == w)。変異 3 本で red
  - 2: 知らせを Advance の後にも取り出す (treefiler/main.go は Update の最後の 1 か所・glogx は `drainFilerNotices` を tick と filerChangedMsg から)。
    tick だけで知らせが出るテストを両方に。変異 2 本で red。🚨 glogx の filerChangedMsg の経路を固定するテストは無い (同じ関数を tick 側が守る)
  - 3: `waitIfQuick` は 1 バイト読んだ後に `stty min 0 time 1; dd bs=64 count=1` で残りを捨てる。pty で ← を押して残りが 0 バイトのテスト (変異で red)。
    0.1 秒以内の 2 つ目のキーと 64 バイトを超える貼り付けの残りは捨てきれない (記録)
  - 4: safefs の `TrashMove`: ゴミ箱の直下の項目を断る (fd の Fstat の比較)・番号を拡張子の前に (`foo 2.txt`。空白を含む・点だけの前半は拡張子にしない)・
    ENAMETOOLONG を理由つきで。doctor にも効く (doctor は `~/.Trash` を対象から外しているので直下の判定は届かない)。変異 5 本で red
  - 5: キー一覧が収まらないとき最後の行に「… 残り N 行」。6: `appendHistory` を O_NOFOLLOW で。変異で red
  - 敵対レビュー (opus) 2 周: 1 周目 P1 = 新しい pty テストが -race で落ちる (Buffer の競合) → lockedBuffer・P2 = clear の範囲が画面の外・空のとき範囲の外の全角を割る →
    切り詰め。2 周目 P1 / P2 なし (clear を 20 万回 × 2 の乱数で幅・範囲の外の不変条件の違反 0)。P3 (全角空白・live の残り・待ちの上限) を直して打ち切った
- 2026-10-12 (dotfiles-4e): 7 を直した (5f の 732 の上に)。commit「fix(treefiler): diff の取得の失敗を「HEAD が無い」と読まず、HEAD が無い repo は空の tree と比べる (issue 731 の 7)」
  - HEAD との diff が失敗したら `gitHeadExists` (`rev-parse --verify -q HEAD`。rc=1 だけを「無い」) で確かめ、無いときだけ **空の tree と作業ツリー** を比べる
    (`hash-object -t tree --stdin` で空の tree を出す。SHA-256 の repo でも合う)。在る・判定できないときは失敗を出す
  - 敵対レビュー (opus): P2 = ref が壊れても rc=1 で「無い」と見える・本物の unborn でも `--cached` は未 stage を隠す (どちらも実測) → 落ちる先を空の tree との差にして、
    判定がどちらに転んでも未 stage を隠さない形にした。P3 = 判定が error を返す場合のテストが無い → 足した。直しは見せる量を増やす向きだけなので周回は打ち切った
  - テスト: `TestDiffHeadFailureIsNotTakenAsUnborn` (HEAD が在る・判定が失敗する)・`TestUnbornDiffShowsUnstagedChanges` (本物の git)・`TestGitHeadExistsOnRealRepo`。変異 4 本で red
