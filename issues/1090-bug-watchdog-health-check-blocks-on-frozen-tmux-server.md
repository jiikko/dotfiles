# 1090 (bug): watchdog の health の確認が固まった tmux サーバに並び、看取りと固まりの撮影が止まる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-19〜）

起票日: 2026-11-19

## 概要

`scripts/tmux_server_watchdog.sh` の `check_health` は、`scripts/tmux_snapshot_health.sh --quiet` を時間の上限なしで待つ。
health スクリプトは tmux に 5 回問い合わせる（`tt_on_default_server` の `display-message`・`tt_resurrect_dir` と `tt_save_interval_minutes` の `show`・`tt_capture_contents_on`・`list-sessions`）。
サーバが固まっていると問い合わせが返らず、その間 watchdog のループが止まる。

## 詳細

- 止まる間は、サーバの死の看取り（`kill -0` の確認）も、1087 で足した固まりの撮影（`check_busy`）も回らない
- 1087 では「直前の窓が忙しい間は health を後回しにする」で避けているが、health の予定が固まりの直前に来たときは並ぶ。
  そのとき撮影は撮らずに `server-busy-missed` 行だけを残す（固まりの最中のスタックを撮り逃す）
- 手で打つ health（`scripts/tmux_snapshot_health.sh` を端末で）は待ってよい。問題は watchdog から呼ぶ経路だけ
- 実測（2026-11-19、本番のサーバ・archive あり）: `--quiet` の所要は 0.21 秒前後（3 回）

## 対応方針

- 問い合わせごとに上限を付けない（5 か所あり、lib の関数を他の呼び出し元と共有している）。watchdog が health を呼ぶところで、health 全体に `bin/runtimeout` で上限を付ける
- 上限は 5 秒（実測の約 25 倍）。窓（5 秒）の 2 倍に届かないので、塞がれても次の窓で連続を数え続け、固まりの最中に撮れる
- 時間切れは ok / ng のどちらにも数えず（状態を変えない）、観測ログに残す
- `runtimeout` は watchdog の起動時に 1 回だけ解決する（`bin/lib/runtimeout.sh`）。解決できなければ、今までどおり上限なしで待ち、その事実を 1 回ログに残す

## 関連ファイル

- `scripts/tmux_server_watchdog.sh`（`check_health`）
- `scripts/tmux_snapshot_health.sh`
- `bin/lib/runtimeout.sh`
- `tests/tmux/test_server_watchdog.sh`

## 進捗

- 2026-11-19: 起票
- 2026-11-19: watchdog の `check_health` で health 全体を `runtimeout` の 5 秒（`TT_HEALTH_TIMEOUT`）で打ち切るようにした。時間切れ（rc=124）は `snapshot-health timeout` 行を残して状態を変えない。`runtimeout` を解決できなければ `snapshot-health-unbounded` 行を残して上限なしで待つ
  - 実装中に、新しい変数に初期値の行が無いと `watchdog_uint_env` の `${!1}` が `set -u` で落ち、watchdog が起動直後に消えることを踏んだ。初期値の行を足し、`${!1:-}` にして未設定でも落ちないようにした
  - [x] テスト (13): 60 秒返らない health を 1 秒で打ち切り、その後も死を看取る。時間切れを ng に数えない
  - 変異で red を確認: 上限を外す / 時間切れの扱いを外す
- 2026-11-19: 敵対的レビュー（opus）。P1 なし。直したもの:
  - 起動時の `runtimeout_resolve` がバイナリが古いと同期で go build し、その間は看取りが始まらない → watchdog ではビルドせず、作ってある `src/runtimeout/runtimeout` をそのまま使う（無ければ上限なしで待って `snapshot-health-unbounded` 行）
  - 時間切れが続くと保存経路の異常が知らされない → 3 回続いたら ng として知らせる
  - ng の toast も tmux に問い合わせて固まったサーバに並ぶ → 背景で起こして待たない
  - 打ち切りの猶予（何も渡さないと 5 秒）で窓の 2 倍に届きうる → `-k 1`
  - テスト: go が無ければ (13) だけを飛ばす / 上限なしの経路 (13b) / 返らない toast でも看取る。変異で red を確認: 3 回で ng に上げる分岐を外す / toast を待つ形に戻す
  - 受け入れたもの: `TT_HEALTH_TIMEOUT` と `TT_BUSY_WINDOW` の組み合わせは照合しない（設定値で、本番では設定していない）/ tmux がサーバの終了時に run-shell のジョブのプロセスグループ全体へ TERM を送るなら、health の最中の死で偽の ng を 1 行書きうる（未確認。tmux のソースで確かめていない）
  - [ ] 本番の watchdog を張り直す
