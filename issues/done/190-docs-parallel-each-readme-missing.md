# 190 docs: `src/parallel-each/` に README が無く、`src/README.md` の案内が指す先が存在しない

起票日: 2026-09-03
重要度: P3
関連: [`_claude/rules/new-tool-requires-entrypoint-docs.md`](../_claude/rules/new-tool-requires-entrypoint-docs.md) /
[issues/done/181](done/181-ux-doctor-entrypoint-docs-missing.md) (doctor に対して同じ形を直した先例)

## 何が起きているか

`src/README.md:3` は「使い方・設計は**各プロジェクトの README を参照**」と案内しているが、
`src/parallel-each/` には **README も CLAUDE.md も無い** (実測 2026-09-03)。

src 配下の他 5 プロジェクトはいずれも README を持つ:

| プロジェクト | 行数 (非テスト) | README | CLAUDE.md |
|---|---|---|---|
| glogx | 22,398 | あり | あり (README が巨大なので「触る前に読む」層が別に要る) |
| **parallel-each** | **4,230** | **無し** | **無し** |
| doctor | 1,844 | あり | 不要 (README が Why を持つ) |
| disassemble_excel | 1,687 | あり | 不要 |
| schedkeys | 1,553 | あり | 不要 |
| lockman | 1,047 | あり | 不要 |

**parallel-each は glogx に次ぐ規模** (22 ファイル / 4,230 行) で、責務もファイル名から
推測するしかない (`dispatch_queue` / `pause_gate` / `precheck` / `result_log` / `tail` /
`scrollable_list` / `plain` / `export`)。主要ファイルの先頭に doc コメントも無い (実測)。

## 何が「無い」わけではないか (誤解を避けるため)

- **使い方は分かる**: `main.go` の `helpText` が 205 行あり、`parallel-each --help` で読める
- **CI はある**: `.github/workflows/src_parallel-each.yml`
- **bin ラッパーもある**: `bin/parallel-each`

つまり欠けているのは「使い方」ではなく **設計の入口** — なぜこの構造なのか、どのファイルが
何を持つのか、触る前に知っておくべき制約。

## 対応 (2026-09-03)

`src/parallel-each/README.md` を作った。`src/README.md` の案内は **6 プロジェクト全部で成立**する
(実測: disassemble_excel / doctor / glogx / lockman / parallel-each / schedkeys のすべてに README がある)。

書いた中身は「設計の入口」に絞った (使い方は `--help` が正本なので写していない):

- **何を解いている道具か** — 並列実行 / 走行中の並列数変更 / リトライ / 再開 / タイムアウト / 項目追加
- **ファイルの責務表** (14 本)。切り出しの理由が「並行の推論を局所化する」ことだと明記した
  (`Runner` の状態変更が散ると race を追えなくなる、という各部品の doc コメントの意図を表に集約)
- **触る前に知る制約** 3 つ — bubbletea v1 のままである理由と上げるときの罠 / charm 依存の版が
  モジュールごとに独立している実測表 / ロックが 2 種類ある理由 (ログディレクトリと入力ファイル)
- **テスト** — 8.7 秒の実測 (2026-09-03 に測り直した) と CI の場所
- **CLAUDE.md を置かない理由**と、置く trigger

### 書きながら直した事実誤認 (実コードで裏取りした結果)

- `Runner` のフィールド数を「30 以上」と書きかけたが**実測 25**。直した
- リトライの区切りヘッダは `=== retry N/M ===` ではなく `=== retry N/M (previous exit=…) ===`
- 「3 モジュールが**同じ** charm 依存を独立に持つ」は誤り。**v2 と v1 でモジュールパスから違う**
  (`charm.land/bubbletea/v2` と `github.com/charmbracelet/bubbletea`)。glogx v2.0.8 / schedkeys v2.0.9 /
  parallel-each v1.3.10 の実測表に置き換えた。`lipgloss` に依存しているのは parallel-each だけ

`make -C src/parallel-each lint` は 0 issues、`test` は 9.8 秒で green。

## やること (完了)

- [x] `src/parallel-each/README.md` を作る。最低限:
  - **何をする道具か** (ファイルの各行に対してコマンドテンプレートを並列実行する TUI) と、
    `--help` を見ればよい範囲は README に写さない (二重管理にしない)
  - **ファイルの責務表** (`dispatch_queue` / `pause_gate` / `precheck` / `result_log` /
    `tail` / `runner` / `tui` / `plain` / `export` あたり)
  - **触る前に知る制約**: bubbletea が **v1 系のまま**であること
    (`docs/glogx-bubbletea-v2.md:113` が「v1.3.10 + lipgloss v1.1.0 のまま (v1 系では最新)」と
    記録している。glogx だけ v2 へ上げた経緯とセットで、なぜ揃えていないかを 1 行)
  - **テスト**: 実測 8.7s で CI に載っている経緯 (`src/README.md:31` が「重いとされていたが実測で
    CI 投入できた」と書いている)
- [x] 書いたら `src/README.md` の案内が全プロジェクトで成立することを確認する

## やらないこと

- **CLAUDE.md は作らない**。`claude-md-maintenance.md` の作成基準 (固有規約 3 個以上 /
  読まないと必ず事故る / README が入口を兼ねられない規模) を満たすのは現状 glogx だけ。
  README で足りるものを 2 枚にすると更新漏れで乖離する
- `--help` の内容を README へ写さない (`main.go` の `helpText` が正本)
