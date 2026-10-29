# 915 (refactor): codex-drive の SKILL.md (1311 行) をフェーズごとのファイルに分ける

起票日: 2026-10-25

epic [910](../910-design-fold-subagents-into-main-loop.md)。911 の eval とは独立に着手してよい (fan-out の構成は変えず、読み込み方だけを変える)。

## 概要

記事の推奨は、手順を skill に置き、必要になったときに読み込む (progressive disclosure)。forge は既に本体 (`SKILL.md` 170 行) と
フェーズのファイル (`phases-0-1.md` / `phases-2-3.md` / `phases-4-review.md` / `phases-5-completion.md` …) に分かれ、
「各フェーズの詳細は開始前に Read」の形になっている。codex-drive は `SKILL.md` 1 本 1311 行で、起動のたびに全体が読み込まれる。

節ごとの行数 (2026-10-25、見出しから次の見出しまで): 大原則 87 / 並列起動の作法 (driver 85 + fallback 33) / Phase 0 58 / D1 99 / D3 30 /
1. スコープ確定 74 / 2. 実装 (コードブロックの中の見出しを含めて) 約 109 / 2p 競作 85 / 3. 検証 48 / 3.5 39 / 3.6 敵対的レビュー 92 / 3.7 テスト強化 約 107 / 3.8 変異 52 /
やること・やらないこと 52。小タスクの軽量パス (D1 の中) で進むときも、2p / 3.6 / 3.7 の全文を読み込んでいる。

fan-out の構成 (D1 の 4 案・3.5 / 3.6 の lens) は記事に照らしても妥当 (独立した探索と、ブラックボックスの検証) なので、この issue では変えない。

## タスク

- [ ] 分け方を決める: 本体に残すのは「役割分担・大原則・工程の厚さ・出力の置き場・ワークフローの一覧・使い分け」と、各フェーズのファイルへの入口だけ。
      候補: `phase-0-fit.md` / `phases-R-S-D.md` (要件・spec・設計) / `phases-1-2-2p.md` (スコープ・実装・競作) / `phases-3-review.md` (3〜3.8) / `parallel-launch.md` (並列起動の作法)
- [ ] 分ける前に、外から節名・節番号で参照している箇所を全部挙げ、移動後も辿れる形にする。一覧は着手時に
      `grep -rln 'codex-drive' bin src _claude tests | grep -v '^_claude/skills/codex-drive/'` で取り直す。2026-10-25 の結果 (14 本):
      - 節名・節番号を引くもの: `bin/codex-fanout` (「並列起動の作法」・`[2p]`・`templates/merger.md` のパス) / `bin/codex-run` (`[2]`・「モデル表」) /
        `_claude/rules/parallel-write-agents-need-worktree-isolation.md` (`[2p]`) / `_claude/rules/mutation-verify-new-tests.md` (`[3.8]`)
      - 名前・skill の存在だけを引くもの (中身を着手時に確かめる): `_claude/rules/claude-md-maintenance.md` / `_claude/CLAUDE.md` / `_claude/skills/codex-review/SKILL.md` /
        `_claude/rules-rationale/verify-execution-not-just-exit-code.md` / `_claude/skill-evals/codex-drive/fires/graders/fires.md` / `src/runtimeout/main.go` (コメント) /
        `src/ratelimit/README.md` / `bin/skill-tmpdir` / `tests/bin/test_skill_tmpdir.sh`
      - 本文を読む検査: `tests/claude/test_codex_drive_effort_consistency.sh` (下の項)
      **節番号 (`[3.8]` など) は振り直さない**
- [ ] `tests/claude/test_codex_drive_effort_consistency.sh` が読むファイルを、分けた後の全ファイルに広げる (本体だけを読む形のままだと、移した節の effort の食い違いが検査から外れる)。
      広げた後に、移した先のファイルで effort を食い違わせる変異を 1 本当てて red を確かめる
- [ ] 読み忘れを防ぐ: 本体が入口だけになると、compaction の後に本体の要約しか残らず、次の Phase のファイルを読まずに進む失敗が起きうる (fires の eval では検出できない)。
      各 Phase のファイルの末尾に「次の Phase に入る前に <ファイル> を Read」を置く。rules の参照 (`mutation-verify-new-tests.md` の「SKILL.md の `[3.8]`」など) はファイル名まで直す
- [ ] `templates/` (merger.md / review-lens-header.md) との関係は変えない
- [ ] 挙動を変えない分割なので、文言は移すだけにする (要約・言い換えをしない)。移す前後で、本文の行の集合が一致すること (見出しの入口の行を除く) を機械で確かめる
- [ ] skill の整合チェック 3 回 + `bin/skill-eval codex-drive` を回す

## 受け入れ条件

- `SKILL.md` 本体が入口と共通の約束だけになり、各フェーズの詳細はそのフェーズに入る前に Read する形になっている
- 外部からの参照がすべて辿れ、effort の整合検査が分けた全ファイルを見ている (変異で red を確認済み)

## 関連ファイル

- `_claude/skills/codex-drive/SKILL.md` / `_claude/skills/codex-drive/templates/`
- `_claude/skills/forge/SKILL.md` (分け方の見本)
- `tests/claude/test_codex_drive_effort_consistency.sh`

## 進捗

- 2026-10-29: 本文を今のコードと突き合わせた。SKILL.md は 1313 行 (起票時 1311)、外からの参照は 14 本で一覧どおり、effort の検査は本体 1 本だけを読んでいた。
- 2026-10-29: 分けた (commit: refactor(codex-drive): SKILL.md を入口と Phase のファイルに分ける)。

  | ファイル | 行 | 中身 |
  |---|---|---|
  | `SKILL.md` | 354 | 役割分担・大原則 (モデル表を含む)・工程の厚さ・出力の置き場・Phase 0・ワークフロー・「フェーズのファイル」の表・使い分け・やること / やらないこと・関連 |
  | `parallel-launch.md` | 127 | 並列起動の作法 (driver / fallback) |
  | `phases-R-S-D.md` | 184 | R・S・D1〜D3 |
  | `phases-1-2.md` | 273 | 1・2・2p |
  | `phases-3-review.md` | 358 | 3・3.5・3.55・3.6・3.7・3.8 |
  | `phases-4-7.md` | 60 | 4〜7 |

  - 案から変えた所: Phase 0 (適性評価) は本体に残した。起動のたびに最初に読む節で、ワークフローの図の `[0]` も「上記」と本体の中を指しているため
  - 本文は移しただけ: 元の 1313 行の多重集合が、新しい 6 ファイルの行から足した行 (各ファイルの頭と末尾の案内・本体の表) を除いたものと一致することを機械で確かめた (取りこぼし 0・予定外の行 0)
  - 例外は 1 行: 本体の「下の『上振れ』」は [2] (`phases-1-2.md`) を指すようになったので、ファイル名で書いた (skill の整合の点検で見つけた。他の「上の / 下の」は同じファイルの中で閉じていた)
  - 各 Phase のファイルの末尾に「次の Phase に入る前に <ファイル> を Read」、頭に「本体から <いつ> Read する」を置いた。本体は「フェーズのファイル」の表で全部を指す
  - 外からの参照を移した先のファイル名に直した: `bin/codex-fanout` (並列起動の作法・[2p]) / `bin/codex-run` ([2]) / `mutation-verify-new-tests.md` ([3.8]) / `parallel-write-agents-need-worktree-isolation.md` ([2p])。
    「モデル表」を引く `bin/codex-run` と codex-review は本体のままなので直していない
  - `tests/claude/test_codex_drive_effort_consistency.sh` を、skill の直下の `*.md` 全部を読む形に広げた (ファイルごとにコードブロックの状態を戻す・`grep -h`・失敗の文の場所)。
    広げた検査は書く run 3 本・読む run 3 本を見つけ、分ける前の本体 1 本に旧い検査を当てた数 (3 本・3 本) と一致した
  - 変異: 移した先 (`phases-1-2.md`) の書く run の effort を high にする → 「write run の起動例が gpt-6-astra / high」で赤 / 検査を本体 1 本だけ読む形に戻す → 赤 (起動例を抽出できない)
  - skill の整合を 3 つの見方で点検し (上の 1 行を直した)、`bin/skill-eval codex-drive` PASS (cases 1/1)。`tests/claude` の 28 本は 26 本が緑で、
    落ちた 2 本は変更と無関係 (worktree では links の検査が skip で落ちる・`~/.config/nvim/coc-settings.json` の壊れた link は分ける前の本体の checkout でも落ちる)。`make test-lint` rc=0
