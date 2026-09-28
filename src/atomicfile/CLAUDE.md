# atomicfile

途中の状態を残さないファイル書き込み (temp + rename) を 1 関数に閉じた module。使い方・設計の正本は README.md と `atomicfile.go` のパッケージ doc / 関数 doc。

## 入口

- `Write(path string, data []byte, perm os.FileMode) error` — 唯一の公開関数。glogx (状態キャッシュ・issue の claim バナー) と ratelimit (利用枠キャッシュ) が replace で取り込む

## ビルド・テスト

- `make -C src/atomicfile lint` / `test`
- 振る舞いのテスト (temp の残骸・失敗経路) は消費者側 (glogx の `cache_test.go` 等) にある。このパッケージ自体に `*_test.go` は無い
