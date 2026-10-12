# 736 (chore): treefiler 周りの依存・CI・lint (2026-10-12 の監査)

> 🚨 **担当中: dotfiles-5f**（2026-10-12〜）

起票日: 2026-10-12

## 詳細

1. **pro-con の `go list -m all` が失敗する** (実測) — `src/pro-con/go.mod` に `replace treefiler => ../treefiler` が無い (glogx が treefiler を require する)。
   `go: malformed module path "treefiler"`・`go mod verify` も失敗。今は pro-con が glogx/issues しか import しないのでビルドは通るが、treefiler に届く package を
   1 つ import した瞬間に落ちる。`go list -m all` を使う道具 (govulncheck 等) は今も落ちる。直し方: replace を足す (届くようになったら src_pro-con.yml の paths も)
2. **glogx の go.mod が `go mod tidy` の並びでない** (実測) — `go mod tidy -diff` が glogx だけ差分 (treefiler / subproc / safefs の行の並び。中身は同じ)。issue 722 で手で
   書き足した並び。tidy を見る gate はどこにも無い (`grep -rn 'tidy -diff'` 0 件)。直し方: glogx を tidy・`go mod tidy -diff` と `go list -m all` を `_go-project.yml` か
   root の lint に足す (1 と同じ穴を両方止める)
3. **(lint-from-done) ユーザーのファイルを動かす・消す直呼びを止める規則が無い** — safefs の「syscall の直前に Guard を通す」は `src/safefs/CLAUDE.md` の文書だけ。
   treefiler の production に `^os\.(Rename|Remove|RemoveAll|Symlink|Link)$`・`^unix\.(Unlink|Rename|Renameat|Mkdir)` の forbidigo を置く案
   (msg「ユーザーのファイルは safefs を通す」)。脅威モデル: うっかりの直呼び。検出しない形: import の別名・`os/exec` の mv / rm・関数値。設定・履歴の自前の
   一時ファイルの `os.Remove` は `//nolint:forbidigo // 理由` で個別に許す。偽陽性の数は未計測 (足す前に数える)
4. 確認済み (問題なし): src_safefs.yml の run 38203353284 で `ok safefs` / `ok safefs/sandbox`・src_treefiler.yml の run 38219350918 で `-race` と仮想化の再走・
   paths filter の漏れ 0 件 (全 `replace ../X` と各 workflow の paths を機械で突き合わせ)。`TestBulkMatchesReadDirLstat` は `-v` でないのでログに名前が出ない
   (macos-15・`//go:build darwin`・Skip 無しなので走った見込み。ログでは示せない)

## 関連

- 737 (この監査)・696 (前回の依存・CI)・722 (glogx の go.mod)

## 進捗

- 2026-10-12: 起票 (監査 737)
- 2026-10-12 (5f): 1〜3 を対応した
  - 全数: 21 module で `go mod tidy -diff` と `go list -m all` を回し、落ちたのは glogx の tidy (rc=1) と pro-con の list (rc=1) の 2 件だけ (issue の記述どおり)
  - 1: pro-con の replace に `treefiler => ../treefiler` を足した (tidy は require を足さない。import していないので)。`scripts/check_go_project_lanes.sh` の
    不変条件 4 に合わせて src_pro-con.yml の paths (push / pull_request) に `src/treefiler/**` を足した
  - 2: glogx を `go mod tidy` (並びだけ。中身は同じ)。gate は `_go-project.yml` の lint の job に step「Module graph (go mod tidy -diff / go list -m all)」を足した
    (21 本の caller が全部これを呼ぶので、その module を触ったときに走る)。root の Lint の job は Go のキャッシュが無く毎 push 走るので置かない。
    手元で修正前の glogx・pro-con が落ちることを確認済み。手元の make lint には入っていない (CI だけ)
  - 3: treefiler の `.golangci.yml` の forbidigo に `os.(Rename|Remove|RemoveAll|Symlink|Link)` と unix / syscall の同類 (Unlink・Rename 系・Rmdir・Mkdir・Symlink・Link 系) を足した。
    今の production の偽陽性は 0 件 (直呼びが 0 件。設定・履歴の置き場の MkdirAll / OpenFile は対象にしていない)。検出しない形 (import の別名・関数値・os/exec の mv / rm) は yml に書いた。
    変異 5 本が red (filer に os.Remove / unix.Unlink / unix.Symlink を足す・main.go に os.Rename を足す。main.go の例外は stdout と「今」だけなので効く)
  - 敵対的レビューは省いた: 判断の処理を持たない設定 (replace の 1 行・tidy・CI の 2 コマンド・lint の pattern) で、どれも修正前後の両方を実物で見た
  - pro-con・glogx・treefiler の lint と test・root の `make test-lint` が緑
