# 160 `_claude/` に rule/hook/skill を足したセッションが `./setup.sh` を忘れて終了する

起票日: 2026-09-02
種別: bug (運用の穴)
関連: [158](158-retro-glogx-dial-bg-and-parallel-sessions-2026-09-02.md) の項目 4 が出典 /
[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)

## 症状

`_claude/rules/*.md` 等を足した commit の後に `./setup.sh` が実行されないまま
セッションが終わると、`~/.claude/rules/` に link が無い状態が残る。

- **その rule は Claude Code から読まれない** (per-file link 方式なので、足しただけでは見えない)
- `tests/claude/test_claude_links_complete.sh` は正しく fail-closed で落ちる (exit 1 を実測)
- しかし**踏むのは次に `make test` を回した無関係な人**。当事者のセッションは既に終了しており、
  後始末の持ち主が不在になる

実例 (2026-09-02): `b6c3b5e` が `_claude/rules/avoid-wall-clock-assertions.md` を足したまま
`./setup.sh` 未実行で終了した (その後 09/02 08:58 に別の実行で解消済み)。

⚠️ **hooks はこの症状の対象外**。hook の起動経路は `_claude/settings.json` の実体パスなので、
link が無くても動く ([142](done/142-research-claude-hooks-link-unreferenced.md))。
実害があるのは rules / skills / agents / commands / workflows。

## 現状の防御と、その穴

| 防御 | 効く範囲 | 穴 |
|---|---|---|
| root `CLAUDE.md` の規律「足したら `./setup.sh` を再実行する」 | 読んだ人が守れば | **忘れたまま終了することを止める仕組みが無い** |
| `test_claude_links_complete.sh` | `make test` を回したとき | **当事者ではなく次の人が踏む**。当事者は既に居ない |

## 対応候補

- **案 A: PostToolUse(Write|Edit) で当事者に即時通知**。`_claude/{rules,skills,agents,commands,workflows}/`
  配下への書き込みを検出し、「`./setup.sh` を実行するまで Claude Code からは読まれない」と返す。
  **当事者に、作った瞬間に届く**のが利点 (既存 `gofmt-on-edit.sh` と同じ形。matcher も同じ)
- **案 B: Stop hook で未リンクを検出して警告**。応答の切れ目ごとに検査するので取りこぼしが少ないが、
  毎回走るコストと「関係ない編集でも鳴る」ノイズがある
- **案 C: SessionStart で未リンクを注入** (`retro-open.sh` / `human-tasks-due.sh` と同じ形)。
  実装が既存の雛形どおりで最も安い。ただし**届く先は「次にセッションを始めた人」**なので、
  当事者不在の問題は解けない (発見は早まる)
- **案 D: 自動修復** (検出したら `./setup.sh` を走らせる)。人手が要らない代わりに、
  setup.sh は dangling 掃除・dir symlink の migrate という**破壊的操作**を含むので、
  自動実行にするなら別途の安全レビューが要る

- **案 E: git の `post-commit` hook** (反証レビュー由来)。commit が
  `_claude/{rules,skills,agents,commands,workflows}/` に触れたら警告する。
  `setup.sh` が (無ければ) インストールする形にする。**状態が変わる点は「Write された」ではなく
  「commit された」**なので、案 A の素通り経路 (下記) をまとめて塞げる。警告は commit を打った
  Bash 呼び出しの stdout にそのまま乗るので、`additionalContext` より確実に読まれる
- **案 F: 案 E + `setup.sh` にリンクだけ張る軽量モード** を足し、post-commit から呼ぶ。
  案 D のブラスト半径 (下記) を避けつつ人手を要らなくできる

## 反証レビューの結果 (2026-09-02) — 案 A は推しから降ろす

観点を分けた 2 体の read-only 反証レビューを通した (codex は使わない設定のため
[`issue-creation-codex-review.md`](../_claude/rules/issue-creation-codex-review.md) の代替手順)。
**事実観点は反証 0 件**。設計観点は案 A に P1 級の穴を 3 つ出した (いずれも実コードで裏取り済み):

1. **素通り経路**: PostToolUse の matcher `Write|Edit` は **Claude Code の Write/Edit ツールしか
   捕まえない**。`cat > ... <<EOF` / `sed -i` / `cp` / `git mv` / `git pull` での取り込みは全て
   Bash ツールなので発火しない。⚠️ **ハーネスが Bash 優先の運用を指示する場合があり、その下では
   案 A の検出対象そのものが日常的に迂回される** (この issue 自身、Bash の heredoc で作られた)
2. **非 blocking**: PostToolUse はツール実行**後**なので原理的に止められない。この repo で block
   しているのは PreToolUse の `deny-bare-tmux-kill.sh` だけ。通知が出た同じターンで従わずに
   終了すれば手遅れで、強制力は `retro-open.sh` の催促と同程度
3. **自己矛盾**: 本 issue は「検査できなかったを緑にしない」と要求しているのに、写し元に挙げた
   `gofmt-on-edit.sh:12` は `command -v jq >/dev/null 2>&1 || exit 0` で**jq 不在時に無音で
   no-op** する (`deny-bare-tmux-kill.sh` / `git-state-verify.sh` も同型)。
   「既存 hook の写しで済む」という推し方自体が設計要求と衝突する

案 B (Stop hook) の追加の穴: 既存テストと同じ「`$HOME/dotfiles` と `pwd -P` の一致」判定を
流用すると、**CLAUDE.md が推奨する worktree 作業では常に skip** される。

案 D (自動 `./setup.sh`) のブラスト半径は本文が挙げた 2 点より**大きい** (実測):
`setup.sh:119` が anyenv の **global** 言語バージョンを切り替え、`setup.sh:150` が Terminal.app の
プロファイルを実際に適用し既定に設定し (`DOTFILES_SKIP_TERMINAL_PROFILE` でしか止まらない)、
`setup.sh:130-143` が legacy symlink を削除する。rule 1 個の編集を trigger にこれを走らせるのは割に合わない。

**現時点の推し: 案 E / F**。ただし案 E にも残る課題がある: `.git/hooks` は clone に付いてこないので
`setup.sh` でのインストールが要る / hook は簡単に無効化できる / **worktree から commit した場合、
是正 (`~/dotfiles` 側へのリンク) はその worktree からは実行できない**。

## 蒸し返さないこと

⚠️ **「`~/.claude/rules` 等をディレクトリ丸ごと symlink にすれば per-file のリンク漏れ自体が起きない」は
既に検討され却下済み**。`a7e9b29` (2026-02-09):「ディレクトリ丸ごとのシンボリックリンクだと
他ツール (ubiregi-cli 等) が配置したリンクと競合するため、skill/agent 単位の個別リンクに変更」。
[`verify-design-intent-before-refactor.md`](../_claude/rules/verify-design-intent-before-refactor.md) の
「既に意図的に選ばれた設計」に当たるので、この issue のスコープでは戻さない。

## 設計時の必須事項

⚠️ [`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) を通すこと。
特に:

- **「検査できなかった」を緑にしない**。`~/.claude` が無い / 別 checkout が `~/dotfiles` の場合は
  skip を明示する (既存テストは exit 77 で skip を出している。同じ扱いにする)
- **hook 自身が落ちても本体を止めない**形にする (既存 hook の timeout 設定に倣う)
- 新設した検査が**実際に発火することを出力で確認する** ([`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md))

## trigger

急がない。`_claude/hooks/` か `setup.sh` を次に触るときに一緒に入れる。
