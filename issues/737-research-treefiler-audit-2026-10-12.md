# 737 (research): treefiler の監査 (2026-10-12。22 種類を直接実行)

起票日: 2026-10-12

## 概要

`/audit treefiler` を 22 種類 (設計 7・品質 8・テスト 2・その他 5 のうち ux を除く) で、読み取り専用の調査体 (sonnet) 5 体を 1 体ずつ順に回した。
前回 (697、2026-10-09) から約 8000 行増えた (712・714・717〜724・726〜729) ので、新しいコード (fileops・safefs・direntries・column・render) を重点に全体を見た。
各体の発見は main (Opus) がコードで裏を取ってから issue にした (重い 3 件は main が再現・確定: git の filter の実行・全角の割れ・Notice の取り出し)。1 つの発見は一番狭い型に 1 回だけ数えた。

| 調査体 | 監査の種類 | 走査した母集合 | 起こした先 |
|---|---|---|---|
| 1 | design / responsibility / duplication / polymorphism / leaky-abstraction / encapsulation / ui-components | filer 非テスト 28 (全文 10・部分 7・grep 11) + main.go + safefs 3 + glogx の接点 | 731 (2)・734・735 (7) |
| 2 | security / broken-code / error-handling / dead-code | filer 非テスト約 20 + main.go + safefs 6 + glogx の接点 + subproc 等 | 731 (3・4・6・7)・732 |
| 3 | resource-leaks / concurrency / false-green / performance | filer 非テスト 28 + safefs 4 + glogx 2 + CI 3 本 + テスト約 45 | 731 (1)・733・735 (1・3) |
| 4 | test-cleanup / test-helpers | テスト 39 ファイル・233 関数。変異 37 本 (有効 34) | 735 |
| 5 | dependency / ci / lint-from-done / general | go.mod 6 つ・CI の run 4 本・issues/done 691〜730 | 731 (5)・736 |

## 全数勘定

- 起こした issue: 731 (bug 7 項)・732 (bug 1。git の filter。repo 全体)・733 (perf 2 項 + 記録)・734 (refactor 3 項 + 記録)・735 (test 7 項 + 記録)・736 (chore 3 項)
- 重複を畳んだもの: 「裏の知らせがキーの後まで出ない」は体 1 と体 2 が別々に出した (731 の 2 に 1 回だけ)
- 0 件だった種類: responsibility (Model 約 70 欄・仮想化 18 欄は分けても結合が減らない)・ui-components (716 が「寄せない」と結論済み)・dead-code
  (`staticcheck U1000 -tests=false`・`deadcode` で 0)・resource-leaks の実リーク・concurrency (`go test -race -count=4` で 0)・test-cleanup の削除 (言い切れたもの 0)

## 攻めたが見つからなかった範囲 (次の監査の起点)

- safefs の dev/ino の照合・NoSymlinkInPath の順序・ValidName・fileops の safeDir・getattrlistbulk の記録の解析 (圧縮・sparse・hardlink・xattr で readEntriesSlow と一致。
  圧縮ファイルの bulk の経路は `ditto --hfsCompression` が効かず未検証)
- 乱数のファイル操作の列 (外の変更と a / m / d / `.` / S / `/` / e) 150 種 × 300 手で panic・不変条件の違反 0。砂場は外への操作を全部実行前に拒否した
- watcher の commit と setDirs・walker の store と forget・gitWatch の again (lock の中で閉じている)。bulkBuf (sync.Pool) は Put の後に参照しない
- 注入: open / git / sh -c の引数は絶対パスか固定。editor.Argv は先頭の - と + を守る。表示する名前・エラーは termsafe を通る
- 依存の版のずれ 0 (x/sys は chromecookie 以外 v0.47.0)・paths filter の漏れ 0 (全 replace と各 workflow の paths を機械で突き合わせ)
- README のキー表・helpKeys・spec §0.1 と treeKeys のずれ 0

## 記録のみ (issue にしない)

- `preview.go` の `ensure` が読むたびにパスで開き直す (FIFO への差し替えで固まる。同じユーザーの競合が前提)・`tile.go` の `resolveLink` が本文のトークンを Stat する
  (autofs で止まりうる。未実測)・`settings.go` の `saveSetting` が rename で書くので `treefiler.toml` が symlink なら通常のファイルに置き換わる (dotfiles は今このファイルを
  管理していない)・`waitIfQuick` の `trap … EXIT` は fish で構文エラー (コメントに未測定と既述)
- 固まったネットワークのマウントを explode / walker / watcher が読むと、`exploding` が残り Busy が戻らない (ReadDir に止める手段が無いことをコードで確認。実機は未確認。
  697 の「切れたマウントで walker が止まる」と同じ根)
- places / treefiler.toml の read-modify-write は後勝ち (複数の treefiler を同時に開いたとき)

## 進捗

- [x] 監査を実行し、発見を 731〜736 に起こした
- [x] 反証レビュー (sonnet・読み取りのみ): 735 の 2 の重さを下げ、3 にコメントの更新を足した。反証できなかったもの: 731 の 1〜3・733 の 1・735 の 1・736 の 1 (コードと実行で一致)。今日の commit で直った項目の混入なし
- 732 で解消 (2026-10-12・5f。表示の status / diff でローカルの設定の filter を止めた。遅延 fetch の別経路は 739)
