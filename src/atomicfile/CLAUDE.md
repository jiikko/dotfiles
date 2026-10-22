# atomicfile

途中の状態を残さないファイル書き込み (temp + rename) を 1 関数に閉じた module。使い方・設計の正本は README.md と `atomicfile.go` のパッケージ doc / 関数 doc。

## 入口

- `Write(path string, data []byte, perm os.FileMode) error` — glogx (状態キャッシュ・issue の claim バナー)・ratelimit (利用枠キャッシュ)・pro-con (状態の置き場)・treefiler (設定・履歴・前回の場所)・mac-lan-manager (台帳・設定・操作のログ) が replace で取り込む
- `IsTemp(name string) bool` — ファイル名が Write の temp の形か。残骸を掃除する側 (pro-con の relay の sweep) が名前の規則を写さずに判定する

## ビルド・テスト

- `make -C src/atomicfile lint` / `test`
- 振る舞いのテスト (temp の残骸・失敗経路) は消費者側 (glogx の `internal/gitdata/cache_test.go` 等) にある。このパッケージ自体に `*_test.go` は無い
