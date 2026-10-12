# safefs

ファイルを動かす・作る操作のうち「上書きしない」「経路を差し替えられない」を守る部品を 1 か所に置く module (macOS 専用)。
doctor (削除エンジンのゴミ箱への移動) と treefiler (`a` 新規作成・`m` 名前の変更・`d` ゴミ箱へ移す) が replace で取り込む。

- `TrashMove` — ゴミ箱へ移す (renameatx_np の RENAME_EXCL・同名は `name 2` と番号を足す・別ボリュームは断る・fd で掴んで (dev, ino) を照合)
- `Rename` / `Create` — 同じフォルダの中での名前の変更と作成 (上書きしない)。`ValidName` が `/` `..` などを断る
- どの操作も呼び出し側の `Guard` を syscall の直前に通す。テストは `safefs/sandbox` で一時ディレクトリの外を実行前に拒否する
  (doctor は自前の 4 段構え = `src/doctor/disk/main_test.go`)
