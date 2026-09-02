# parallel-each — ファイルの各行に対してコマンドを並列実行する

入力ファイルの 1 行を 1 ジョブとして、コマンドテンプレートの `{item}` に差し込み、指定並列数で走らせる。
走行中は TUI で進捗・失敗・ログ末尾を見て、並列数を変えたり項目を足したりできる。

**使い方 (オプション・キー操作・ログ形式) は `parallel-each --help` が正本**。
`main.go` の `helpText` に 205 行あり、この README には写さない (二重管理を作らない)。
ここに書くのは **設計の入口** — どのファイルが何を持つか、触る前に知る制約。

## 何を解いている道具か

長時間かかる同種の処理 (ダウンロード・変換・API 呼び出し) を大量の入力に対して回すとき、
次が全部必要になる。それを 1 つにまとめたもの。

- 並列実行と、走らせながらの**並列数変更**
- 失敗の**リトライ** (既定 5 回。1 ジョブ 1 ログファイルに `=== retry N/M (previous exit=…) ===` で区切って残す)
- 中断した後の**再開** (`parallel-each-log/result.log` に済みの項目が入っているので、次回はそれを飛ばす。
  やり直したいときだけ `--fresh`)
- タイムアウト (試行ごと `--attempt-timeout` は必須。全体は `--total-timeout` で任意)
- 走行中の**項目追加**と、結果の export

## ファイルの責務

`main.go` / `runner.go` / `tui.go` の 3 本が本体で、残りはそこから切り出した部品。
**切り出しの理由は「並行の推論を局所化する」**: `Runner` は 25 フィールドの並行オーケストレータで、
状態の変更が 12 箇所に散ると race の検討が追えなくなる。各部品は自分の mutex を持ち、
`Runner` の他の状態には触らない (だから単体でテストできる)。

| ファイル | 持つもの |
|---|---|
| `main.go` | フラグ解析 (`Config`)、`helpText`、入力ファイルの読み込み、TUI と plain の振り分け |
| `runner.go` | 実行の中核 (`Runner`)。worker の起動、リトライ、タイムアウト、ログディレクトリと入力ファイルの flock、`Event` の発行 |
| `tui.go` | bubbletea の `model`。進捗・スロット・最近の結果・各種プロンプト (並列数変更・項目追加) の状態機械 |
| `plain.go` | TUI を使わない経路 (`--no-tui` / 非 TTY)。行ベースの出力とシグナル処理 |
| `dispatch_queue.go` | 受理済みだが worker へ渡していないジョブの順序付きリスト。不変条件をこの型の mutex に閉じる |
| `pause_gate.go` | 一時停止の可逆ゲート。停止中は `waitUntilResumed` で dispatcher を止める。停止信号は channel で注入する |
| `result_log.go` | TAB 区切りの `result.log` への thread-safe な書き込み。`close → rewrite → reopen` を mutex を保持したまま行う |
| `tail.go` | ログ末尾の読み出し。末尾 8KB だけ読むので大きいログでも安全。ヘッダ行 (`# ` と `---`) は除く |
| `scrollable_list.go` | スクロールするリストのカーソルと viewport (`listState`)。データへの参照は持たず、件数とページ幅を呼び出し時に受ける |
| `precheck.go` | live-add の入力の静的チェック。`--input-type=url` のとき、scheme 無しや貼り付けミスをネットワークに触らず弾く |
| `format.go` | 端末の**表示幅**での切り詰め (`truncate`)。溢れたら `…` を付ける |
| `export.go` | 結果の書き出し。`writeWrapperFn` がテストの差し込み点 (ファイルシステムに触らずに出力を捕まえる) |
| `editor.go` | 外部エディタの起動と終了通知 (`editorDoneMsg`) |
| `events.go` | `Runner` から TUI / plain へ流すイベントの型 (`Event` / `EventKind`) |

## 触る前に知る制約

### bubbletea は v1 系のまま (glogx だけ v2 へ上げた)

`go.mod` は `bubbletea v1.3.10` + `lipgloss v1.1.0` (v1 系では最新)。glogx は v2 へ移行済みだが、
**ここは意図的に上げていない**。理由と移行の手順は [`docs/glogx-bubbletea-v2.md`](../../docs/glogx-bubbletea-v2.md)
の「他の Go プロジェクトはまだ v1」節が正本。

⚠️ 上げるときの最大の罠: `tea.KeyMsg` / `tea.KeyRunes` の参照が glogx より桁違いに多く、
**space が `" "` → `"space"` に変わる差はコンパイルエラーにならない** (静かに壊れる)。
キー操作の目視確認をセットにして、1 モジュールずつ上げる。

### charm 依存を持つ 3 モジュールがそれぞれ独立に版を持つ

`go.mod` はモジュールごとなので、**バージョンの揃え忘れは構造的に起きる**。揃える仕組みは今は無い。
実測 (2026-09-03):

| モジュール | bubbletea | x/ansi |
|---|---|---|
| glogx | `charm.land/bubbletea/v2 v2.0.8` | v0.11.7 |
| schedkeys | `charm.land/bubbletea/v2 v2.0.9` | v0.11.8 |
| **parallel-each** | `github.com/charmbracelet/bubbletea v1.3.10` (**import パスが違う**) | v0.10.1 |

v2 は `charm.land/bubbletea/v2`、v1 は `github.com/charmbracelet/bubbletea` で、**モジュールパスから違う**。
`lipgloss` に依存しているのは parallel-each だけ (v2 の 2 本は `ultraviolet` に移っている)。

### ロックは 2 種類ある

- **ログディレクトリ**: `<LogDir>/.lock` に排他 flock。`Start` から `cleanup` まで保持し、`Close` で解放する
- **入力ファイル自体**: `cfg.File` に排他 flock。これは advisory で、他の `parallel-each` プロセスにだけ効く
  (エディタや `cat` は影響を受けない)。**「同じ `-F` に別の `--log-dir`」**という、
  ログ側のロックだけでは検出できない形を捕まえるためにある

## テスト

```
make -C src/parallel-each lint
make -C src/parallel-each test
```

CI は [`.github/workflows/src_parallel-each.yml`](../../.github/workflows/src_parallel-each.yml)
(paths filter 付き、lint と test を別 job で)。

**「TUI 依存だから重い」は実測で崩れている**: 全体で **8.7 秒** (2026-09-03 実測、`go test ./...`)。
CI から除外する理由は無い。テストコードは本体より多く (`runner_test.go` 2,203 行 / `tui_test.go` 2,165 行)、
並行と状態機械の主張はそこで固定している。

## CLAUDE.md を置いていない理由

[`claude-md-maintenance.md`](../../_claude/rules/claude-md-maintenance.md) の作成基準
(固有規約が 3 個以上 / 読まないと必ず事故る / README が入口を兼ねられない規模) を満たすのは、
今のところ `glogx` だけ。README で足りるものを 2 枚に分けると、片方の更新漏れで乖離する。
**この README が入口を兼ねられなくなったとき**が CLAUDE.md を作る trigger。
