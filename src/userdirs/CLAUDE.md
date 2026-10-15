# userdirs

道具ごとの設定の置き場 (`ConfigDir`) と、古い置き場からの 1 回きりの引っ越し (`MoveIfMissing`)。使い方は README.md、設計の理由は `userdirs.go` のパッケージ doc。

- 置き場を道具ごとに分け、glogx の下に置かないのはユーザーの決定 (2026-10-16。道具を将来 glogx から分離するため)
- `make -C src/userdirs lint` / `test`
