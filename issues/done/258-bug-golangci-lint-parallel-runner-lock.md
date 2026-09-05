# golangci-lint のグローバル file lock で、並行実行すると lint が落ちる

起票日: 2026-09-05
カテゴリ: bug
優先度: 中

## 症状

golangci-lint は起動時にグローバルな file lock を取り、別インスタンスが走っていると
`parallel golangci-lint is running` を出して失敗する。src/ の 7 プロジェクトは
`.golangci.yml` に `run:` 節を持たず、`allow-parallel-runners` が未設定のため既定 (false) で動く。

実害は 2 つある。

1. **並行セッション間で `make test` が落ちる**。同じマシンで 2 セッションが `make test` を
   同時に回すと、後発の lint が落ちる。実例 (別セッション dotfiles-c6 の報告、2026-09-05):
   `make test` を 2 本並行させて src/disassemble_excel・src/glogx・src/parallel-each の
   3 つが落ち、直列で回し直すと rc=0
2. **`run_go_projects` の lint を並列化できない**。59f9e48c で go test は並列化したが、
   lint はこの lock のため直列のまま残した

## 実測 (2026-09-05、14 コア機、同一 checkout)

`make -C <dir> lint` を同時起動し、rc を観測した。

| 条件 | 試行 | 結果 |
|---|---|---|
| **7 本同時** (全プロジェクト) | 4 ラウンド | **4/4 で再現**。毎回 2〜3 本が rc=2 + `parallel golangci-lint is running` |
| **2 本同時** (組を変えて反復) | 7 ラウンド = 延べ 14 実行 | **0 件**。1 本も落ちない |
| `make test` を 2 本並行 (別セッション dotfiles-c6 の報告) | 1 回 | 3 本失敗。直列で回し直すと rc=0 |

**同時数が効いている**。2 本同時は延べ 14 実行で 1 本も落ちず、「機会の数が増えれば 2 本でも
踏む」という読み方は**実験で否定された**。

🚨 3 行目 (c6 の報告) は `make test` の中で lint が直列に回るので、字面どおりなら同時に走る
golangci-lint は最大 2 本のはずで、上の 2 行目と矛盾する。ただし**この観測は条件が分離されて
いない**: 同時刻に私 (dotfiles-a2) が同じマシンで 7 本同時の計測を回しており、第 3 の
golangci-lint が走っていた。**2 本同時の反例としては採用しない**。

**rc=2 は `make` の終了コード**で、golangci-lint 自身の終了コードは make に隠れて分離できて
いない (必要なら `go run ... golangci-lint run` を直接叩いて測り直す)。

固定バージョンは 7 プロジェクトとも `GOLANGCI_LINT_VERSION := v2.5.0`。
**閾値 (何本から踏むか) は未測定**。2 と 7 のあいだは測っていない。

## 対応案

各 `src/*/.golangci.yml` に次を足す:

```yaml
run:
  allow-parallel-runners: true
```

これは lock を無効化するのではなく「並行して走ってよい」と宣言するもの。7 プロジェクトの
契約変更になるため 59f9e48c のスコープには入れなかった。

## 着手前に確かめること

- 🚨 **`allow-parallel-runners: true` が何をマスクしていた failure mode を外すのかを先に列挙する**
  (`_claude/rules/list-masked-failure-modes-before-removing-guard.md`)。この lock は
  「同じキャッシュディレクトリを複数インスタンスが同時に書く」ことを防いでいる可能性があり、
  外した結果がキャッシュ破損や結果の取りこぼしなら、落ちる方がまだ安全。
  **v2.5.0 のドキュメントと実装で、この lock が何を守っているかを確認してから入れる**
- 入れたら `run_go_projects` の lint も並列化できるか実測する (`$(if $(filter lint,$(1)),;,&)`
  の分岐を外せるか)。**速くなったと書く前に before/after を測る**
- 検証は **7 本同時を複数ラウンド**。現状 4/4 で落ちるので、修正後に 4 ラウンド連続で
  0 本ならば効いたと言える (2 本同時は元から落ちないので、検証条件に使わない)
- 測るときは **他セッションが同じマシンで lint を回していないこと**を確かめる
  (上表 3 行目がその混入で条件不明になった)

## 関連

- `59f9e48c` — go test の並列化と、lint を直列に残した理由 (Makefile の `run_go_projects` 直上コメント)

## 対応 (2026-09-05, dotfiles-c6)

### lock が守っていたもの (v2.5.0 のソースで確認)

`pkg/commands/run.go` `acquireFileLock`: `os.TempDir()/golangci-lint.lock` への flock。
`allow-serial-runners` が false なら 5 秒でタイムアウトして "parallel golangci-lint is running"。
`allow-parallel-runners: true` は lock 取得と解放を丸ごとスキップする。

**キャッシュ保護ではない**。golangci-lint のキャッシュ (`internal/go/cache`) は Go の
`cmd/go/internal/cache` の fork で、内容アドレス + 一時ファイル → rename の原子書き込み。
複数プロセスからの同時アクセスを前提に設計されているので、lock を外しても結果の正しさには
効かない。flag の説明も "Allow multiple parallel golangci-lint instances running. If false
(default) - golangci-lint acquires file lock on start." だけで、キャッシュへの言及は無い。

つまりこの lock が**マスクしていた failure mode は「同時起動による CPU / メモリの重複消費」だけ**
(IDE 連携で多重起動する事故の抑止が起源と思われるが、それは未確認の推測)。7 本同時の
ピーク RSS は下表のとおり 1 プロセスあたりでは問題にならない。

### 入れたもの

- 7 プロジェクトの `src/*/.golangci.yml` に `run.allow-parallel-runners: true` (理由コメント付き)
- `Makefile` `run_go_projects` の lint 直列分岐 (`$(if $(filter lint,$(1)),;,&)`) を外して並列に
- `tests/scripts/test_golangci_parallel_runners.sh`: 7 プロジェクト全部に設定があることを pin。
  1 つでも抜けると**そのプロジェクトだけが他の起動タイミング次第で落ちる** (flaky に見える) ので、
  新しい src/ を切ったときの漏れをここで止める。変異 (lockman から設定を削除) で red を確認

### 実測 (2026-09-05、14 コア機、`make test-go-lint` の通し)

| 条件 | 所要 | lock 衝突 |
|---|---|---|
| before (lint 直列、27df01a0) 新規 worktree の 1 回目 (キャッシュ冷) | 19 秒 | — |
| before (同) 2〜3 回目 (キャッシュ温) | **7 / 8 秒** | — |
| after (7 本並列) round 1〜6 (キャッシュ温) | **3 / 2 / 3 / 2 / 2 / 3 秒** | **6 ラウンドとも 0 行**、全プロジェクト rc=0 |

比較に使うのは温まった状態どうし: **直列 7〜8 秒 → 並列 2〜3 秒**。
検証条件は本文どおり「7 本同時を複数ラウンドで 0 本」で、6 ラウンド連続 0 本。

計測条件の注記:
- 最初に取った before (19 秒、10:13) は別セッション (dotfiles-a2) の `make test` 末尾 (go lint) と
  重なっていたので比較から外し、a2 が lint を止めている窓 (10:22 以降) で 3 回測り直した。
  1 回目の 19 秒は新規 worktree のキャッシュ冷でも同じ数字になるので、汚染の有無は数字からは
  切り分けられない (どちらにせよ比較には使わない)
- after の round 1〜4 (10:14〜10:15) は a2 の計測とは重なっていない (a2 の終了 10:13:52)
- ピーク RSS は `/usr/bin/time -l` で before 1.07 GB / after 0.13 GB と出たが、並列の子プロセスの
  rusage を同じ形で数えているか確認していないので**比較には使わない** (未比較)
- 閾値 (何本から踏むか) は測っていない (対応後は踏まないので不要)
