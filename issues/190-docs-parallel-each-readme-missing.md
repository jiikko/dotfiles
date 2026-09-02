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

## やること

- [ ] `src/parallel-each/README.md` を作る。最低限:
  - **何をする道具か** (ファイルの各行に対してコマンドテンプレートを並列実行する TUI) と、
    `--help` を見ればよい範囲は README に写さない (二重管理にしない)
  - **ファイルの責務表** (`dispatch_queue` / `pause_gate` / `precheck` / `result_log` /
    `tail` / `runner` / `tui` / `plain` / `export` あたり)
  - **触る前に知る制約**: bubbletea が **v1 系のまま**であること
    (`docs/glogx-bubbletea-v2.md:113` が「v1.3.10 + lipgloss v1.1.0 のまま (v1 系では最新)」と
    記録している。glogx だけ v2 へ上げた経緯とセットで、なぜ揃えていないかを 1 行)
  - **テスト**: 実測 8.7s で CI に載っている経緯 (`src/README.md:31` が「重いとされていたが実測で
    CI 投入できた」と書いている)
- [ ] 書いたら `src/README.md` の案内が全プロジェクトで成立することを確認する

## やらないこと

- **CLAUDE.md は作らない**。`claude-md-maintenance.md` の作成基準 (固有規約 3 個以上 /
  読まないと必ず事故る / README が入口を兼ねられない規模) を満たすのは現状 glogx だけ。
  README で足りるものを 2 枚にすると更新漏れで乖離する
- `--help` の内容を README へ写さない (`main.go` の `helpText` が正本)
