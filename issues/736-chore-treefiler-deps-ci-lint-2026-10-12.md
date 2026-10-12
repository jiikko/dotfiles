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
