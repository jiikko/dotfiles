# 419 (test): glogx のテストが本物の docker を叩いていて、実コマンドの検査もそれを見ていない

起票日: 2026-09-24
反証レビュー: 2026-09-24 実施 (読み取り専用のサブエージェント 1 体)。主要な主張は反証できず

## 概要

glogx のテスト用ヘルパー `installInertDoctor` (`src/glogx/tui_helpers_test.go`) は、doctor の走査口のうち
disk / svc / brew を無害なものに差し替えているが、**`dockerOpts` だけ差し替えていない**。そのため
`dockerCmd` (`src/glogx/doctor_docker.go`) が本物の `runner.Exec` を使い、テストの中で実マシンの
`docker system df --format json` が走る。issue 216 の「テストは実マシンのコマンドを叩かない」に反する。

さらに、それを見張る検査 `tests/glogx/test_no_real_commands_in_tests.sh` は、shim を置くコマンドが
`brew xcrun pgrep launchctl du` だけで **`docker` が入っていない**。検査自身がこの退行を観測できない。

出典: 2026-09-24 の設計・品質スキャン (観点 4「並行処理のデータ競合」)。スキャンでは PATH の shim の記録で
docker が呼ばれることを確認し、起票者はヘルパーと検査の shim 一覧をコードで確認した。

## 詳細

- 影響するテスト (スキャン報告): TestDoctorEnterMovesCursorIntoItems / TestDoctorSelectSingleDirectory /
  TestDoctorEntrySelectionSupersedesItems / TestUpdateKeysYieldToDoctorDelete (2 回) /
  TestDoctorLiveDiskScanSanitizesRealFileNames
- 差し替え用の値は既にある: `noDockerOptions` (`src/glogx/doctor_view_test.go`。`doctor_docker_test.go` が使っている)
- `docker system df` は読み取りだが、Docker Desktop が起動していない開発機では daemon への接続待ちになりうる
  (テストの所要が環境で揺れる。未実測)

### 観点 4 の他の結果 (本番のデータ競合は見つからなかった)

- `-race` の全テスト実行 1 回と、Update / View を 1 goroutine・各 Cmd を別 goroutine で 4 秒走らせる模擬テスト
  (diff・issues・status・doctor の約 600 メッセージ) で、データ競合は出なかった。意図的に仕込んだ競合は検出できたので、
  ハーネスは競合を見られる
- **P3 (読んだだけ・未確認)** 一部の Cmd の closure が、テスト用の差し替え口 (`loadTmuxPrefix` / `runGitPush` /
  `runGitPullRebase` / `runClaudeUpdate` / `openInBrowser` / `loadWorktreeStatus` / `loadCommitDiff` など) を
  **実行時に**読む。本番では再代入されないので競合しない。テストでは `t.Parallel()` を使っていないが、
  `initCLIHealthMsg` (`src/glogx/cli_health_test.go`) と `toast_integration_test.go` が子の Cmd の goroutine を
  置き去りにする。置き去りの goroutine が差し替え口を読むと、次のテストの差し替えと競合しうる
  (今の置き去りは値をコピー済みの Tick だけなので起きない)。`checkCLIVersionCmd` / `spawnAutobuildCmd` のように
  作る時点でコピーすれば閉じる
- **P3 (潜在・到達はほぼ不可能)** 後始末の latch の `add()` が Cmd の goroutine の中で呼ばれる (pull と doctor の
  svc / brew / docker / delete / cmd-run)。Cmd を返してから goroutine が始まるまでに終了が割り込むと、
  `waitPullCleanup` / `waitDoctorCleanup` が `add()` より先に返りうる。窓はマイクロ秒で、main は先に
  `cancelAll` を呼ぶ。Update の中で `add()` してから Cmd を返せば構造的に閉じる
- 安全と確認したもの: Cmd は gen / epoch / seq / repo / sha / colored / opts を作る時点でコピーしている。
  Cmd に渡すスライスは毎回新しく作る。status のプレビューのキャッシュ・lineCache・issues のポインタは
  Update / View でしか触らない。`hlEscCache` は sync.Map、`probeSeq` は atomic。fsnotify のイベント Cmd は
  watcher と gen を値で持つ

## 対応方針

1. `installInertDoctor` に `v.dockerOpts = noDockerOptions` を足す
2. `test_no_real_commands_in_tests.sh` の shim の一覧に `docker` を足す。**先に 2 を入れて、1 の前に red になる
   (docker の呼び出しが記録される) ことを確かめてから 1 を入れる** (検査が退行を観測できることの確認)
3. P3 の 2 件は、直すかどうかを判断して記録する

## 関連ファイル

- `src/glogx/tui_helpers_test.go` (`installInertDoctor`) / `src/glogx/doctor_docker.go` / `src/glogx/doctor_view_test.go`
- `tests/glogx/test_no_real_commands_in_tests.sh`
- 関連 issue: 216 (テストが実マシンのコマンドを叩く) / 214 (走査 goroutine がテストを跨ぐ競合)

## 進捗

- [ ] 検査に docker を足して red を確認
- [ ] installInertDoctor に dockerOpts
- [ ] P3 の 2 件の判断
