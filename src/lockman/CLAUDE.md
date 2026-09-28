# lockman

ディレクトリ単位の排他を取る CLI (SMB 越しの複数マシン + 公開ホストのローカル経路混在を想定)。仕様の正本は issue 091、実装の要点と現状 (v1 / 実機で未検証の前提) は README.md。

## ファイルの地図

- `main.go` — エントリポイント。フラグ解析 (`parseFlags`)・サブコマンド分岐 (`dispatch`: acquire / release / renew / check / status / with / break / cleanup)
- `lock.go` — 排他の中核 (`Locker`)。原子操作での取得・期限切れの引き継ぎ (rename)
- `lease_tracker.go` — `leaseTracker`: 「自分の lease が生きていると言い切れる限界」を持つ状態機械。期限の値を単体で検査できるよう分離
- `timeout.go` — `--io-timeout` の配線 (`withTimeout` 等)
- `cleanup.go` — 残骸 (scratch / graveyard) の掃除。lock 本体は触らない (期限切れの引き継ぎは `lock.go` の `Acquire` だけが行う)
- `with.go` — `with` サブコマンド (子プロセスを排他区間で実行)
- `util.go` — token 生成などの小道具
- `ab_abandoned.sh` — `--io-timeout` で見捨てた goroutine の副作用を実バイナリで A-B 計測するスクリプト (`go test` では測れない。issue 362)

## テスト

- 大半は同名実装の隣 (`*_test.go`)
- `timeout_wiring_test.go` — 横断検査。新しい I/O 経路が `--io-timeout` で包まれているかを `go/ast` で静的に検査する (脅威モデルと検出しない形はファイル冒頭のコメントが正本)
- `on_lost_*_test.go` / `takeover_window_test.go` など、実装ファイルと対にならない名前のテストは異常系・競合窓のシナリオ (ファイル名が再現する failure mode を示す)

## 入口

- `bin/lockman` (自動ビルドつきラッパ。**`--async` は使わない** — 旧版バイナリが誤って「取れた」を返す危険があるため同期ビルド)

## ビルド・テスト

- `make -C src/lockman lint` / `test`

## 詳しくは

- README.md (現状・実機で未検証の前提・対象環境・CI が検証しない範囲・`ab_abandoned.sh` の使い方)
- issue 091 (仕様の正本)
