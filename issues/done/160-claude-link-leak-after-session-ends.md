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

## 決着 (2026-09-02)

**採用: SessionStart hook で「欠けているときだけ」link を張る** (案 C + 案 F の合体。案 E の git hook は不採用)。

- `scripts/claude_links.sh` — 期待するリンク集合の唯一の出典。`check` (変更なし。0 = 揃っている /
  1 = 欠けあり / 3 = 検査不能) と `apply` (欠けた分だけ `ln -sfn`。0 / 2 = 張れないものあり / 3)
- `_claude/hooks/claude-links-sync.sh` — SessionStart で `check`。揃っていれば無言 (fork 無しの
  builtin 判定だけ)。欠けていれば `apply` して張った内容を additionalContext で報告。常に exit 0
- `setup.sh` は同じスクリプトを `apply` で呼ぶだけにした。**掃除 (dangling 削除) と migrate は
  setup.sh にだけ残す**

**なぜ案 E (git post-commit / post-merge) でなく hook か**: E は警告を読んで `./setup.sh` を打つ人が
要る = 忘れる余地が残る。SessionStart なら忘れても次の起動で直る。加えて E は `pull.rebase=true`
のこの環境では post-merge が発火せず post-rewrite も要る、`.git/hooks` は clone に付いてこない、
worktree から commit すると是正できない、と契機の穴が多い。SessionStart は「rule がどう届いたか
(commit / pull / cp)」を問わない。

**案 D (setup.sh 丸ごと自動) の却下理由はそのまま有効**。切り出した `apply` は破壊的操作を持たない:
- `~/.claude/<dir>` が dir symlink (旧形式) なら 1 件も張らず exit 2 (`ln -sfn` がリンク先 = repo 側へ
  書き込み、元ファイルを自己参照 symlink で壊すため。setup.sh の migrate が前提)
- link 先に symlink でない実ファイル / `_claude/` 以外を指す symlink (他ツールの link。a7e9b29 の
  衝突がこれ) があれば上書きせず exit 2 で報告
- dangling は消さない (setup.sh の責務。`test_dangling_symlinks.sh` が検出する)

**「検査できなかった」を緑にしない**: `~/.claude` / `_claude` が無ければ exit 3 で、hook は
「点検できなかった」を注入する。スクリプト不在 (配線ミス) も注入する。無言なのは「揃っている」
だけ。

**未実測**: SessionStart hook が張った rule が**その**セッションで読まれるか (rules の読み込みと
hook の実行順)。報告文は「遅くとも次のセッションから有効」と保守的に書いた。実測は「rule を足した
直後のセッションで、その rule の内容が context に在るか」を見れば分かる。急がない。

**検証**: 実環境で hook が自分自身 (`claude-links-sync.sh`) の link を補い、2 回目は無出力になる
ことを実行で確認。unit テスト `tests/claude/test_claude_links_sync.sh` (13 ケース。素通り側と
過剰側の両方) + 変異検証 + 観点を分けた敵対レビュー (結果は commit message に記載)。

### 敵対レビューの結果 (2026-09-02、観点を分けた read-only 2 体)

- **壊す観点: P1/P2 なし**。実測で出た事実: `ln -sfn` は dest が**実ディレクトリ**だと `-n` が効かず
  `dest/<basename>` として中へ潜り込む (BSD ln)。これを防ぐのは `apply` の「symlink でない実体なら
  refuse」1 行だけなので、テストに実ディレクトリ (skills/sk1) のケースを足し、ガードを外す変異で
  red を確認した。readlink の前方一致は `..` 入りで理論上すり抜けるが、書き込む値はスクリプトが
  計算した正しい実体なので実害は「他ツールの link が正しい link に置き換わる」に留まる (記録のみ)
- **素通り観点: P2 1 件、却下**。「skills/<name>/ に SKILL.md が無くても check は揃っている扱い」。
  この機構は **link の有無**を保証するもので、中身の妥当性は対象外 (旧 setup.sh も同じ)。skill の
  中身検査は別の道具の責務なので、ここでは扱わない
- 両者が確認した「素通りではなかった」項目: timeout 10 秒に対し実測 20ms 前後 (95 件) / lib 欠落時も
  フォールバックで黙らない / オラクル test_claude_links_complete.sh と期待集合が一致 (95 件で実測)
