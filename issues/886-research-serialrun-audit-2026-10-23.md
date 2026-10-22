# 886 (research): serialrun (issue 869) の 22 観点の監査 — 全数勘定と、直さないと決めたもの

起票日: 2026-10-23

## 概要

serialrun に audit skill の全 22 観点 (design / responsibility / duplication / polymorphism / leaky-abstraction / encapsulation / ui-components / ux /
security / resource-leaks / broken-code / dead-code / error-handling / false-green / performance / concurrency / test-cleanup / test-helpers /
dependency / ci / lint-from-done / general) を 1 体ずつ直列で回した (2026-10-22 22:3x〜2026-10-23 01:xx。5h 枠のリセット待ちで 9 観点目の後に 1 回止めた)。
報告 22 本を 1 体で突き合わせ、直す項目を [878](878-bug-serialrun-exclusion-and-order.md)〜[885](885-chore-serialrun-ci-deps-perf.md) に分けた。ここには全数勘定と、
直さない候補 (I) とその理由を残す (却下の理由が消えると、次の監査が同じ指摘を出す)。

## 監査の途中で起きたこと

- 監査のサブエージェントがテストを回すたびに、失敗・時間切れのテストが子の `serial-run` と `sh` を残した (PPID=1)。test-cleanup の監査が当てた機械的な変異
  (main.go / queue.go に 215 本) のうち、`stateBase` の `SERIAL_RUN_STATE_DIR` の判定を壊すものでは、テストの `serial-run` が**本物の待ち列** (`~/.local/state/serial-run`) に入り、
  他の session (dotfiles-0e の mutate-verify) を 11 分止めた (推測。原因の変異は特定していない)。孤児 34 本と子・`sh` 129 本を pid 指定で止めた。直すのは [883](883-test-serialrun-test-gaps-and-isolation.md) の F (テストの子の HOME / XDG_STATE_HOME も一時 dir へ・テストが本物の置き場を使おうとしたら拒否する)
- 一時 dir を `$TMPDIR` の直下に作ったサブエージェントは、後片付けを hook (`deny-tmp-area-rm`) に止められて残した (4 個。ユーザーに `/cleanup-tmp-dir` を頼んだ)。後半は `tmp/serialrun-20261022/` の下に作らせた

## 0. 全数勘定

### 0-1 観点ごとの発見数 (報告の見出しを 1 つずつ数えた。P1 は全 22 本で 0 件)

| 観点 | P2 | P3 | 未確認 | 計 | 備考 |
|---|---|---|---|---|---|
| broken-code | 1 | 3 | 3 | 7 | |
| ci | 0 | 2 | 0 | 2 | |
| concurrency | 1 | 3 | 2 | 6 | |
| dead-code | 0 | 3 | 0 | 3 | |
| dependency | 0 | 3 | 0 | 3 | P3-3 は報告自身が「発見ではなく観測ポイント」 |
| design | 3 | 4 | 2 | 9 | |
| duplication | 1 | 2 | 1 | 4 | |
| encapsulation | 0 | 4 | 2 | 6 | 冒頭に「P1/P2 は無し」 |
| error-handling | 1 | 6 | 2 | 9 | 未確認の 2 本目 (appendHistory 等の `_ =`) は報告自身が「報告対象にしない」 |
| false-green | 2 | 3 | 1 | 6 | |
| general | 2 | 5 | 2 | 9 | |
| leaky-abstraction | 1 | 3 | 1 | 5 | |
| lint-from-done | 3 | 3 | 0 | 6 | P3-3 は報告自身が「候補から外す」 |
| performance | 2 | 3 | 0 | 5 | P3-2・P3-3 は「不要 / 問題ではない」 |
| polymorphism | 0 | 2 | 1 | 3 | |
| resource-leaks | 1 | 3 | 1 | 5 | |
| responsibility | 1 | 2 | 0 | 3 | |
| security | 1 | 3 | 2 | 6 | |
| test-cleanup | 2 | 6 | 0 | 8 | 見出しは P3 を 6 本並べるが、報告の件数欄は「P3 4 / 未確認 1」で食い違う。見出しで数えた |
| test-helpers | 3 | 4 | 0 | 7 | |
| ui-components | 0 | 6 | 0 | 6 | P3-6 は「(未確認)」付き。P3 に数えた |
| ux | 2 | 5 | 1 | 8 | |
| **合計** | **27** | **78** | **21** | **126** | P1 = 0 |

(件数は表の各行の和と、P2 27 + P3 78 + 未確認 21 = 126 の両方で合わせた。)

### 0-2 まとめた後

- 発見 126 本 → 直す項目 **66** (A 11 / B 11 / C 4 / D 9 / E 10 / F 17 / G 2 / H 2) + 直さない候補 I **18** = **84 項目**
- 126 本のうち、報告自身が発見でないと書いたもの (error-handling U-2 の `_ =`) は I18 に置いた。他の 125 本は全部どれかの項目の出典になっている (付録の対応表で全本を引ける)
- 重要度 (項目の中の出典の最大): P2 22 項目 / P3 40 項目 / 未確認のみ 4 項目 (A11・B7・C3・F9) / I 18 項目 (重要度は付けない)
- 複数の観点が同じものを出していた項目 (出典 2 観点以上): A1・A3・A5・A6・A10・B1・B3・B5・B6・B7・C1・D1・D2・D3・E1・E2・E9・E10・F1・F6・G1・G2 ほか

### 0-3 直し方の優先 (私の読み。報告の範囲)

先にやる価値が高い順: A1 (排他が黙って破れる。実験済み・1 行の修正) → C1 (無視の継承。確認済み) → A4 (SIGSTOP のまま永久に残る。確認済み) → B1+B2 (hook の偽陰性・偽陽性。実機で既に出ている) → D3 (計測 5 の母集合が偏る。869 の未完了項目に直結) → F2・F1 (変異で緑・孤児の sh が実機に 9 本)。

---

## I. 直さない候補 (設計で受け入れたもの・コストに見合わないもの・発火を示せないもの)

### I1 重い 5 系統 (make test / go test / swift build,test / xcodebuild / mutate-verify) の元の字面に掛けた deny・ask の許可ルールが、書き換えで外れる
- 理由: 869 が「記録だけ (決めるのはユーザー)」とした P2 そのもの。作者の実測 (Claude Code 2.1.308)「許可ルールは書き換えた後のコマンドで判定される」の引用で、security は再現していない (推測)。現状 `permissions.deny / ask` は 0 件
- 869 の場所: 869:173 (実測) / 869:303-306 (記録だけ。他の repo の settings に `Bash(make test:*)` 2 件・`go test:*` 4 件・`make lint:*` 2 件) / 869:314 (Makefile 組み込みで字面が `make test` のままになれば解消)。general の「未確認リスク」の 1 本目 (他 repo の許可ルール) も同じ
- 残す作業: README と hook の冒頭に「deny / ask は `serial-run -- …` の字面でも書く」を 1 行書く (E8)。(b) hook が permissions を読んで元の字面が当たるなら書き換えない案は、決めるのがユーザーなので出さない
- 出典: security P2-1 / general U-1

### I2 子が正常に終わった後に背景の孫を回収しない (孫は lock の外で走り、次の run と同時に動く)
- 理由: 869:136「子が正常に終わった後は子孫を止めない。残った孫は lock の外で走る (受け入れる。`status` には出ない)」。runtimeout も同じ設計。回収するなら背景に回す意図の呼び出しを壊すのでオプションになる
- 確認済み: `serial-run -- sh -c 'sleep 6 & …'` は rc=0 で戻り孫が生存、直後の 2 本目は待たずに走った。孫が stdout/stderr を継承すると Bash tool が EOF を待つ構成では呼び出しが返らない (未実測)
- 残す作業: README に 1 行 (E8)
- 出典: resource-leaks P3-1

### I3 TERM / HUP の子孫停止が、停止の前にすでに reparent された孫 (`( cmd & )` / `setsid` / `nohup cmd &`) を止められない
- 理由: ps の親子関係で木を組む方式の限界で、macOS に cgroup が無く確実な手段が無い。通常終了でも同種の孫が残る (I2) ので新しい害ではない。プロセスグループに置かない設計 (README の 🚨) とも両立しない。869 には明記が無い
- 確認済み: `trap "" TERM` の sh の下の `( sleep 30 & )` は生き残った (rc=143・札は消えた)
- 残す作業: README に「切り離された孫は止めない」
- 出典: resource-leaks P3-2

### I4 history.log が無制限に伸びる
- 理由: 1 行 約 135 B、1 日 300 本で約 60 KB / 年 22 MB の見積もり。読み手 (status・hook) は history を読まず実害はディスクだけ。design 報告も「実害の発火条件を示せず」。放置でよい (直すなら起動時に 1 MB 超で `history.log.1` へ rename)。869:72 は「追記だけ」と設計
- 出典: resource-leaks P3-3

### I5 `ticketState` の性質 (flock で待てるか / 走っているとみなすか / 何と表示するか) を 3 か所の比較・switch が個別に持つ
- 理由: 今は 3 値で全部整合 (確認: 読み)。薄い置き換えは複雑性を移すだけ。trigger は 4 つ目の state を足すとき (D1・A2 で `stateUnknown` の扱いを変える commit で結果的に整理される可能性はある)
- 出典: polymorphism P3-1

### I6 別 version の serial-run が同じ置き場を使う (札の形式・採番の規則を変えて `git pull` すると旧版と新版が食い違う)
- 理由: 履歴上は 1 版 (208049e3 から queue の形式は不変) で今は顕在化しない。869 に互換の契約は書かれていない。形式を変える時点で、置き場の名前に版を入れるか互換の契約を 869 に書く判断が要る (未確認)
- 出典: concurrency U-2

### I7 `acquireRun` が timeout / signal で抜けるとき `rf.Close()` するが、`blockLock` の goroutine は `flock(rf)` で眠ったまま
- 理由: 直後に `main` が `os.Exit` するので実害を示せなかった (発火しない)。`run` を長寿命プロセスから呼ぶ形 (ライブラリ化) にしたときだけ顕在化する (未確認)
- 出典: resource-leaks U-1

### I8 `acquireRun` と `waitPredecessors` の select (deadline / sigs / report) の腕が 2 か所に写し
- 理由: 今は両方とも deadline → waitTimeout / sigs → waitSignal で一致。共通化は 2 つの待ちの性質 (イベント駆動の再列挙 / 1 回の取得) が違うので読みにくくなるかもしれず、発火条件を実例で示せない (未確認)
- 出典: duplication U-1

### I9 状態の置き場・seq・run.lock・ticket を `O_NOFOLLOW` も所有者・perm の検査も無しで開く (置き場が別ユーザーに書ける dir だと symlink で任意のファイルを 13 byte 上書きできる)
- 理由: 脅威モデル (同じユーザーの別 session と誤設定) の対象外。既定は `~/.local/state/serial-run` (0700)。共有 dir を指す SERIAL_RUN_STATE_DIR / XDG_STATE_HOME では成立する (未確認)。README に前提を書く (E8)
- 出典: security U-1

### I10 hook は `ToolInput` を写して `updatedInput` を返すので、他の PreToolUse hook も `updatedInput` を返すと後勝ちか競合かは Claude Code の実装に依存する
- 理由: 現状の他の hook (deny-*) は書き換えないので発火しない (未確認)。書き換える hook を足したときに確かめる
- 出典: security U-2

### I11 lint の共通部 (`scripts/golangci_lint.sh` など 6 種) だけを触る commit で、serialrun を含む 24 プロジェクトの test job が全部走る
- 理由: 1 本あたり約 40 秒と安い。`_go-project.yml` のコメントは共通部の変更を全プロジェクトで検証することを意図している。paths を分けると「paths を足し忘れて test が走らない」の新しい口ができる (tests/ に整合の検査を同時に置く必要)。割に合うかは未判断 (直近 30 日で 12 commit)
- 出典: ci P3-1

### I12 replace 先 (proctree) の依存の追従が「手で足す 2 箇所」に頼る
- 理由: 検出手段が揃っている (`go mod tidy -diff`・`scripts/go_module_graph.sh`・go_autobuild の指紋が replace 先を含む)。今の依存 0 件が崩れる日に赤くなることだけ確認すればよい。報告自身が「発見ではなく観測ポイント」
- 出典: dependency P3-3

### I13 `bin/serial-run` が毎回 Go を 2 回起動する (`__ready` → `exec`)・待ち札の数への依存は小さい
- 理由: 追加起動は約 3 ms で、包まれた重いコマンド (秒〜分) には無視できる。待ち札 60 枚で +2 ms (1 枚あたり約 0.03 ms の線形)。孤児札が多数のときの `procStart` は未実測 (見積もり 100 枚で 250 ms)
- 出典: performance P3-2・P3-3

### I14 検査 (lint / forbidigo / check script) を新設しない決定: クロスモジュールの同型関数の検査・`Process.Kill` の forbidigo 禁止・`ps -o lstart` の LC_ALL 検査・`errcheck.check-blank` の共通化・SERIAL_RUN_STATE_DIR だけの隔離の検査
- 理由: lint-from-done の判断。(1) 同型関数の検査は本体一致が 3 組で全部同じ 2 module の組 → 共有化 (G1) で終わらせ、2 組目が出たら足す。(2) `Process.Kill` の禁止は 9 箇所中 1 つしか実害を確認しておらず偽陽性が多い → F1 の helper 経由で足りる。(3) lstart の検査は該当 1 行 → A1 で直すだけ (trigger: 2 箇所目の ps の文字列比較が出たとき)。(4) check-blank は共通に入れると偽陽性が多い (serialrun の local だけなら A3 で検討)。(5) SERIAL_RUN_STATE_DIR だけの隔離は serialrun 自身が TestMain と fixture `env()` で二重に隔離済みで発火経路を作れなかった。INT/QUIT の Notify の検査 (C1) だけは helper 化までのつなぎとして残る
- 出典: lint-from-done P2-2 の検査案 / P2-3 の検査案 / P3-1 / P3-2 の検査案 / P3-3

### I15 待ちが長いときに「いつもは約 N 分」(history.log の同じ cmd の ran= の中央値) を添える
- 理由: 新機能の提案で、効果は推測 (報告も「欠落は確認、効果は推測」)。D3 (history の母集合) が直ってからでないと中央値が偏る。要望が出たら足す
- 出典: ux P3-1

### I16 エラー文の接頭辞 `serial-run: ` を手書きで 10 箇所に書いている
- 理由: 今は一致していて困っていない。2 つの CLI のために共通 package を作るのは過剰 (報告の評価)。やるなら serialrun の中だけで `errf(stderr, ...)` 1 つ
- 出典: ui-components P3-5

### I17 shell 側の `expect` / `FAIL_COUNT` 集計が tests/claude の他のテスト 3 本と同形にコピーされている (tests/lib に assert の共通 lib が無い)
- 理由: コピーが 3 本ではまだ足りず凍結 (報告の判断)。このファイルは `exit 1` を守っており現状は無害。tests/lib に `assert_eq` / `finish` を足す提案まで
- 出典: test-helpers P3 (shell の expect)

### I18 `appendHistory` / `logWake` / `printStatus` の `_ =` と、`leave()` の `os.Remove` の失敗の握りつぶし
- 理由: 報告自身が「情報用で、捨てても排他・順番は壊れない。報告対象にしない」と書いた (error-handling の件数欄は未確認 2 に数えている)。`leave()` の Remove 失敗も次の待ち手の `stateOf` が stateGone として扱うので順番は壊れない (読んで確認)
- 出典: error-handling U-2

---

## 付録: 126 本 → 項目の対応表 (全数の検算用)

broken-code: P2-1 C1 / P3-1 B1 / P3-2 D2 / P3-3 A3 / U-1 B7 / U-2 A5 / U-3 G2
ci: P3-1 I11 / P3-2 H1
concurrency: P2-1 A4 / P3-1 A5 / P3-2 A7 / P3-3 A10 / U-1 A11 / U-2 I6
dead-code: P3-1 B10 / P3-2 E10 / P3-3 D6
dependency: P3-1 H2 / P3-2 B5 / P3-3 I12
design: P2-1 B8 / P2-2 C2 / P2-3 A6 / P3-1 D1 / P3-2 E9 / P3-3 A8 / P3-4 B9 / U-1 C3 / U-2 E1
duplication: P2-1 G1 / P3-1 D7 / P3-2 A1・E10 / U-1 I8
encapsulation: P3-1 D5 / P3-2 A6 / P3-3 D1 / P3-4 D4 / U-1 D1 / U-2 B3
error-handling: P2-1 A2 / P3-1 G2 / P3-2 D1 / P3-3 C4 / P3-4 A6 / P3-5 A9 / P3-6 A10 / U-1 A3 / U-2 I18
false-green: P2-1 F2 / P2-2 B6 / P3-1 F3 / P3-2 F4 / P3-3 F5 / U-1 F9
general: P2-1 B2 / P2-2 D3 / P3-1 E8 / P3-2 B3 / P3-3 B4 / P3-4 F8 / P3-5 E8 / U-1 I1 / U-2 B7
leaky-abstraction: P2-1 A1 / P3-1 D8 / P3-2 A5 / P3-3 E7 / U-1 B7
lint-from-done: P2-1 C1 / P2-2 G1 (+I14) / P2-3 F1 (+I14) / P3-1 A1 (+I14) / P3-2 A3 (+I14) / P3-3 I14
performance: P2-1 B5 / P2-2 B5 / P3-1 H2 / P3-2 I13 / P3-3 I13
polymorphism: P3-1 I5 / P3-2 E10 / U-1 B3
resource-leaks: P2-1 F1 / P3-1 I2 / P3-2 I3 / P3-3 I4 / U-1 I7
responsibility: P2-1 B1 / P3-1 B5 / P3-2 D2
security: P2-1 I1 / P3-1 D9 / P3-2 E9 / P3-3 A6 / U-1 I9 / U-2 I10
test-cleanup: P2-1 B11 / P2-2 F6 / P3-1 F6 / P3-2 F6 / P3-3 F7 / P3-4 F6 / P3-5 F10 / P3-6 F11
test-helpers: P2 zombie F12 / P2 waitFor F13 / P2 インライン F14 / P3 札のパス F15 / P3 否定の確認 F16 / P3 hookContext F17 / P3 shell の expect I17
ui-components: P3-1 E6 / P3-2 E5 / P3-3 E4 / P3-4 E7 / P3-5 I16 / P3-6 D1
ux: P2-1 E1 / P2-2 E2 / P3-1 I15 / P3-2 E3 / P3-3 C4 / P3-4 E2 / P3-5 E2 / U-1 E1

(表の項目数: A 11 / B 11 / C 4 / D 9 / E 10 / F 17 / G 2 / H 2 / I 18 = 84。)
