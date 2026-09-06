# research: resource-leaks 監査の記録（2026-09-06）— 全数勘定・却下理由・未決着

起票日: 2026-09-06
カテゴリ: research
出典: `/audit` resource-leaks / forge Minimum+（`go-architecture-designer` /
`architecture-reviewer` / `test-coverage-advisor` の 3 体 + クロスレビュー 2 体 + 統合 1 体、
計 7 体・約 47 分）

この issue は**却下した指摘とその理由**を残すためのもの。残さないと次の audit が同じ指摘を
再生成して、反証コストを丸ごと払い直すことになる
（[`move-report-conclusions-to-issues.md`](../_claude/rules/move-report-conclusions-to-issues.md)）。

## 全数勘定

| | 件数 |
|---|---|
| 生指摘（3 体の合計） | 22 |
| クロスレビューで除外 | 5 |
| クロスレビューで追加された指摘 | 7 |
| 統合後（High 1 / Medium 11 / Low 15） | 27 |
| **issue 化した** | **10**（298 / 299 / 300 / 301 / 302 / 303 / 304 / 305 / 306 / 307。うち 304 は human） |
| **main agent の検閲で却下した** | **1**（下の ①。issue 化しなかった） |
| 記録に留めた（下の②〜⑨） | 8 |
| この research issue | 1（308） |
| 未決着（両論併記。下の「未決着」節） | 5 |

## main agent が実測で裏を取ったもの

fan-out の主張のうち、私が**自分で再現した**もの:

| 主張 | 実測結果 |
|---|---|
| `$TMPDIR/schedkeys-jobs.*` = 14,147 個 | ✅ 一致（`schedkeys.*` = 5 / `glogx-test-cache*` = 40 / 全 18,297） |
| local + シングルクォート EXIT trap は正常復帰後に空展開する | ✅ `TRAP saw=[]`。関数実行中の TERM では `TRAP saw=[<path>]` |
| 並列ランナーへ TERM を送ると孫が残る | ✅ 偽 make で再現（fakemake 2 + 孫 4 が生存） |
| zsh は TERM で EXIT trap を走らせない | ✅ zsh 5.9 で TERM のみ残留、INT / HUP は消える。bash は TERM でも消える |
| issue-progress hook に prune / TTL が無い | ✅ grep 0 件。増加ペースは**このプロジェクトだけで 1 日約 100 セッション**（transcript 実測: 直近 7 日で 676 件）。監査の「月 1000」は過小 |
| WaitDelay ゲートの実効検査数 | ✅ 非テストの `exec.CommandContext` は 2 件、うち 1 件は marker で skip = 実質 1 件。`exec.Command(` 7 件は未検査 |
| tmux 孤児サーバ | ✅ 隔離 socket 6 本（最長 29 日）。監査の「7 本」には**本番サーバのセッションが混ざっていた** |

## 却下した指摘（理由つき。再生成防止）

### ① glogx の `reflectGitLogChange → loadLogData` に ctx が無い → **false positive**

監査は「done/146 が timeout を付けない理由とした『起動時の同期経路と同じ関数を使う』は
refactor 後に成立していない」と主張したが、**私が裏を取った結果これは誤り**。

- 据え置き理由の主語は `loadLogData` ではなく **`LoadCommits`**
  （`gitlog_watch.go` の注記: 「🚨 LoadCommits は timeout なしの runGit を使う（起動時の同期経路と
  同じ関数）」）
- **`main.go:114` は今も `LoadCommits` を直接呼んでいる**。つまり起動経路と非同期経路は
  実際に同じ関数を共有しており、**理由は今も成立している**
- 監査は `loadLogData` の呼び出し元だけを数えて `LoadCommits` と取り違えていた

done/146 は「追従だけを timeout で救う形は採らない」を理由つきで受容済みなので、
[`verify-design-intent-before-refactor.md`](../_claude/rules/verify-design-intent-before-refactor.md)
に従い**再提案しない**。ハング自体も再現できていない（未確認リスク）。

**ただし 1 点だけ残る**: `gitlog.go` の doc は「TUI 対話中に非同期発行される経路は `runGitTimeout` を
使うこと」と**全称で**書いており、`gitlog_watch.go` の受容と文面が食い違う。
直すなら `gitlog.go` 側に「例外は `gitlog_watch.go` の追従経路（理由は同ファイル）」の 1 行。
**コード変更は不要**。

### ② `_concat.zsh` のプリフェッチに同時実行数の上限（窓 8）を入れる → 実測なしの先回り最適化

数百件が渡された観測が挙げられておらず、ローカルファイルなら各プロセスは即終了する。
pid を全部 `wait` しているのでリークでもない。**trigger 待ち**（クラウド上の多数ファイルで
concat が固まったという報告が出たら実測して窓を入れる）。実装するなら pid 配列の手作り窓より
`xargs -0 -P 8 -n1` の方が状態を持たず単純。

### ③ `cli_health` を `cleanupLatch` / `cancelAll` に載せる → 受容済み

`cli_health.go` の関数 doc が既に「`cancelAll` と結び付けないのは既存のバージョン検査・
`usage.Fetch` が Background+timeout の契約で揃っているため。検査群全体を終了時 cancel へ揃える
改修時に見直す」と**設計判断と再検討条件を明記**している
（[`pending-issue-rationale-in-code.md`](../_claude/rules/pending-issue-rationale-in-code.md) の要求形を満たす）。
指摘者自身も実害を否定（Setpgid 無し + CLOEXEC パイプで子は次の出力で EPIPE）。
露出は `cliHealthTimeout` の 5 秒上限で閉じる。

### ④ `_dotfiles_check` の「pid が生きていない古い result を掃除する」→ 重すぎ

実害（シェル 1 個につき毎プロンプトの glob 1 回、シェル寿命で有界、残骸実測 0B）に対して
新しい破壊的経路を 1 本増やすうえ、**同じ監査が別項で自ら挙げた pid 再利用の窓**を持ち込む。
掃除は `zshexit` に閉じる（issue 300）。

### ⑤ `run_make_targets_parallel.sh` の `kill -- -$$` → 危険

`#!/bin/sh` で make から呼ばれるため `$$` は pgid ではない。**make ごと、あるいは無関係な
グループへ TERM が飛ぶ**。採るべきは `set -m` + `kill -TERM -"$pgid"`（issue 301 に否定形で記録済み）。

### ⑥ `doctor` の削除インベントリに prune を新設 → コメントに留める

`src/doctor/disk/delete.go:DefaultHistoryDir`。production の読み手が 0 件（forensics 専用）、
増加は年単位で KB（実測 4 ファイル / `~/.cache/glog` 全体 88K）。
同 repo に**同じ判断の正本**がある（`scripts/tmux_schedule_keys.sh` 冒頭:
「掃除機構を置かないのは意図的: 残骸は数バイト…掃除は破壊的操作の新設なので、溜まった証拠が
出てから作る」）。`issues/done/163` が観点として挙げたまま判断されていないので、
**`DefaultHistoryDir` 直近に却下理由と再開 trigger をコメントで残す**のが対応
（prune を作るのは trigger が来てから）。

### ⑦ av1ify のテスト根拠「Test 1-6 は単体しか見ていない」→ 数え違い

実際は Test マーカー 37 個で Test 7-9 は統合まで見ている。**核心は正しい**ので指摘自体は
issue 306 として採用し、根拠の記述だけを除外した。

### ⑧ default socket ゲートの fail-open → 単純反転は禁止（未確認リスクとして記録）

`scripts/lib/tmux_resurrect_guards.sh:tt_on_default_server` は判定不能を合格（fail-open）に
丸めている。**しかし単純な反転は別の無音故障を作る**: `periodic_save` / `watchdog` は
`run-shell -b` からしか起動されない（`_tmux.conf:624,630`）ので、`display-message` が空を返す
状況は「隔離サーバ」ではなく「**何かが壊れている**」側であり、そこで `exit 0` すると
**本番の周期スナップショットが黙って止まる**。

repo 自身のルール（判定不能を allow / deny のどちらかに丸めない）に従い、**第 3 の値として
`tt_trigger_log` へ残してから向きを決める**（ログ経路は既にある）。倒す向きを変えるなら、
`tmux` を PATH 先頭の偽コマンド（空を返す）に差し替えたハーネスで挙動を実験で固定すること。
**発火は再現できていない**ので issue 化しない。

### ⑨ `doctor` の `Runner` func 型を消費側で宣言して依存を切る → trigger 待ち

Go の func 型は構造的に代入可能なので、`disk` / `svc` / `docker` が自前で `type Run func(...)` を
宣言すれば `runner` への import は消える（インターフェースは消費側に置く、の一般則）。
今は leaf への依存で実害が小さいので**先回りで分解しない**。
**trigger**: doctor に 2 つ目の実行バックエンド（record/replay や remote）が要るとき、または
`disk` / `svc` を doctor の外から使いたくなったとき。`runner.go` に
「現状は意図的に leaf 共有、再評価の trigger は上記」と 1 行残す程度でよい。

## 攻めたが 0 件だった範囲

- **`src/glogx` / `src/doctor` の goroutine リーク・fd リーク・子プロセスリーク: 新規 0 件**。
  前日の issue 273 監査が悉皆で潰している。残ったのは ctx 配線（上記①で却下）と
  検査ゲートの射程（issue 303）だけ
- **TUI の外部実行**は Background+timeout の契約で統一されており、終了時 cancel へ揃える改修は
  `cli_health.go` に trigger 付きで受容済み（上記③）
- **`src/doctor` の外部実行の口は `runner/runner.go` の 1 箇所だけ**で、型（`Options.Run` への注入）で
  守られている。ここに字句ゲートを足しても検査対象は 1 箇所で、ほぼ何も守らない

## 未決着（両論併記。着手時にユーザー判断）

1. **WaitDelay ゲートの軸** — 字句パターンの拡張 vs import 境界検査（issue 303 に詳述）
2. **`_dotfiles_check` の修正方針** — `$$` 化 vs pending カウンタ廃止（issue 300。後者は
   そのまま実装すると通知が永久に出なくなる）
3. **`claude-issue-progress` の severity** — 実測 1 ファイルで low か、設計が無限成長なので medium か
   （監査内部でも「実測 40 個」の Go テスト一時 dir を low に置いており、**severity 軸が揃っていない**）
4. **`doctor-history`** — prune 新設 vs 却下理由コメント（上記⑥。私は後者を推す）
5. **`schedkeys` の真因** — trap の不作動 vs 正常復帰パスの rm 欠落（issue 298 で**後者に決着済み**。
   実測を載せたので再燃させないこと）

## 🚨 監査そのものの構造的な弱さ（次回への申し送り）

3 体のクロスレビューが**独立に同じ構図**を指摘した:

- **修正案のうち 5 本が「新しい破壊的操作」の新設だった**（env 由来パスへの `rm -rf` /
  `find -delete` / pid ベースの掃除）。いずれも
  [`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)
  §0-A（作らずに済む構造はないか）を**1 件も問うていない**。
  schedkeys は実際「正常復帰パスに `rm -f` を 1 行足す」だけで 14,147 個の 100% が止まる
- **severity の根拠がファイル件数に一元化**されていた。露出境界（mode / 所有者 / 置き場）や
  実害の質を分けていないため、件数の多いものが自動的に上位に来る
