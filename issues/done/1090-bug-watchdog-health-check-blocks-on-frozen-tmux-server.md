# 1090 (bug): watchdog の health の確認が固まった tmux サーバに並び、看取りと固まりの撮影が止まる

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
- 2026-11-19: 敵対的レビューの 2 周目（opus）。P1 なし。直したもの:
  - 置いてあるが動かない runtimeout（別アーキテクチャ・壊れたファイル）を使うと、毎回 126 などで ng に読まれ、本物の health が二度と見られない → 起動時に `runtimeout 0 true` で確かめ、だめなら上限なしで回して `reason=runtimeout-broken` を残す
  - 時間切れが続いた ng の後、固まりが解けて本物の異常が見えても、ng のままなので理由が書かれない → 状態を ok / ng / ng-timeout の 3 つにし、変わったら書く（文言では比べない。「最後の保存が N 分前」のように毎回変わる数が入る）
  - health を止めている（`TT_HEALTH_CHECK_INTERVAL=0`）ときは unbounded 行を出さない / docs の作り方の誤り（素の `make` は runtimeout を作らない）
  - テスト: toast を起こしたことを印で確かめる（toast は watchdog から TERM の無視を引き継ぐので、テストは KILL で止める）/ (13b) は上限なしで health が実際に回ることを、無い場合と動かない場合の両方で見る。変異で red を確認: 動くかの確かめを外す / ng-timeout を ng に戻す / toast を起こさない
  - 受け入れたもの: go が無い環境では (13) を飛ばして緑になる（🚨 の行を出す。CI には go がある）/ watchdog が KILL で死ぬと、その時の health が上限まで孤児で残る（変異の run で 1 回見た。上限の秒数で終わる）
  - [x] 本番の watchdog を張り直す（2026-11-19。前の watchdog の pid を名指しで USR1 で止め、`tmux run-shell -b` で張り直した。`src/runtimeout/runtimeout 0 true` は rc=0 で、起動時に unbounded 行も watchdog-bad-env 行も出ていない）
- 2026-11-19: CI。fix の commit（「fix(tmux): 動かない runtimeout を起動時に見分け…」）の Tests / Lint / Bench は、後の push の concurrency で取り消された（cancelled）。
  その commit を祖先に持つ e0ffd980d の Tests（run 44511070725）・Lint（44511070737）・Bench は success
  - 未確認のまま残したもの（起票していない）: `scripts/tmux_snapshot_health.sh` は tmux に繋がらないと `@resurrect-dir` の代わりに `$HOME/.tmux/resurrect` を見るので、
    `@resurrect-dir` を設定している環境では「last が無い」の偽の異常を出しうる（2 周目のレビューの指摘。手元では確かめていない）
