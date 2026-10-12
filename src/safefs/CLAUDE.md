# safefs

上書きしない作成・名前の変更・ゴミ箱への移動 (macOS)。使い方は README.md、制約は各関数の doc。

## 触る前に読むもの

- 🚨 新しい操作を足すなら、syscall の直前に `check(guard, …)` を通す (nil の Guard は失敗にする)。テストは `sandbox.Sandbox` を Guard に渡し、
  TestMain の最後に `Violations` が空であることを見る (`ops_test.go` / `src/treefiler/filer/main_test.go`)
- 🚨 振る舞いを変えると doctor の削除にも効く。`make -C src/doctor test` も回す (ゴミ箱の上書きしない・symlink の経路・砂場の検査は doctor 側のテストにもある)

## ビルド・テスト

- `make -C src/safefs lint` / `test`
