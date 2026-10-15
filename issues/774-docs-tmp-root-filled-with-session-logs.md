# 774 (docs): `tmp/` 直下にセッションの検証・git 操作ログが溜まる — 置き場の規約に「名前」と「消す人」が無い

起票日: 2026-10-15

## 概要

`~/dotfiles/tmp/` の直下に、Claude のセッションが書いた使い捨てのログが 1170 ファイル (136 MiB) 溜まっている。
中身は push / pull / rebase / lint / `make test` の出力をリダイレクトしたもので、**どの issue / docs からも参照されていない**。
原因は特定の skill ではなく、規約が「出力はファイルへ落とせ」「置き場は `./tmp`」までしか言わず、
**ファイル名の付け方 (どの階層に置くか) と、誰がいつ消すか**を決めていないこと。

## 実測 (2026-10-15)

- 直下のファイル 1170 件 / 136.44 MiB。ディレクトリ 51 件は別 (mutation の worktree・サンプル等。こちらは本 issue の対象外)
- 更新月はすべて 2026-10 (`find tmp -maxdepth 1 -type f -exec stat -f '%Sm' -t '%Y-%m' {} + | sort | uniq -c`)
- 拡張子: `.log` 979 / `.txt` 112 / `.out` 21 / `.sh` 11 / その他
- 名前の形はほぼ全部 `<issue番号か略称>-<工程><連番>.log` (`548-push.log` / `670-pull3.log` / `zk-push10.log` / `638-maketest2.log`)。
  先頭の prefix は約 360 種類 (= 書いたセッション・作業の数の目安。数え方で 359〜375 に振れる:
  `find tmp -maxdepth 1 -type f | sed 's|tmp/||' | grep -E '\.(log|txt|out|err)$' | sed -E 's/-.*//' | sort -u | wc -l` → 359)
- 工程の内訳 (名前による粗い分類。正規表現次第で ±10% 動く): git 操作 (`push` `pull` `rb` `rebase` `claim` `done` `mv` `links` `sync`) 450 /
  検証 (`lint` `test` `maketest` `mut` `ci` `bench` 等) 264 / その他 456
- 参照: 直下の全ファイル名で `grep -rlF "tmp/<名前>" issues docs _claude` → **0 件**

### zundamon-kaisetsu の残骸か?

**skill の出力ではない**。`_claude/skills/zundamon-kaisetsu/SKILL.md` の手順 3 は作業ディレクトリを
`./tmp/<名前>/` (サブディレクトリ) に取る設計で、その出力は `tmp/it-kaisetsu/` のようにディレクトリへ入っている。
直下の `zk-*.log` / `ci-zk-*.log` は **zundamon-kaisetsu (の Go 実装) を開発したセッションの** push / pull / lint /
`make test` / bench のログで、他の issue の作業 (`548-*` / `670-*` …) と同じ形をしている。

## 原因

1. **「出力はファイルへ落とす」規約が出力先の名前を決めていない**。
   - `_claude/rules/verify-execution-not-just-exit-code.md`: 「検証コマンドの出力を `tail` / `head` / `grep` で削って読まない。ファイルへ落としてから読む」
     「前段は `> log 2>&1; rc=$?` で取り」
   - `.claude/rules/worktree-per-session.md`: 「push の rc は `> log 2>&1; rc=$?` で直接取る」
   - これを守ると 1 回の作業で push / pull / lint / test ごとに 1 ファイルずつ生まれ、再試行のたびに連番が増える (`670-push3.log`)
2. **置き場の規約が直下を既定にしている**。`_claude/CLAUDE.md`「一時ファイルの配置」は「成果物 (レポート・スクラッチ・中間生成物) は `./tmp`」と言い
   (scratchpad は「例外: そのまま使ってよい」と許すだけで、推奨していない)、
   さらに「後で消す worktree の中で走らせる出力は、**本体 checkout の `tmp/` を絶対パスで書く**か scratchpad に置く」と言う。
   dotfiles はセッションごとに worktree を切る (`.claude/rules/worktree-per-session.md`) ので、**全セッションのログが `~/dotfiles/tmp/` 直下の 1 か所に集まる**
3. **消す責任がどこにも無い**。同じ節は「線引きは置き場所ではなく終了時に消す責任が実装されているか」と書くが、
   それは「スクリプト / テストの隔離ディレクトリ」側の話で、セッションが手で書いたログには誰も消す責任を持たない。
   `make clean-tmp` (`scripts/clean_tmp.sh`) は手動起動で、既定は 30 日より古いものだけ — 今ある 1170 件は全部 30 日以内なので対象外
4. ログの用途と寿命が合っていない。push / pull / lint のログは**同じ手順の中で書いて rc と中身を読んだら役目を終える** (`_claude/issue-rules.d/move-report-conclusions-to-issues.md` の表の 1 行目
   「同じ手順の中で書いて読む出力先」)。セッションを越えて残す理由が無いのに、セッションを越えて残る場所 (gitignore された repo 内) に置いている

対比: `scripts/issue_claim.sh` は push の出力を `"$work/push.log"` (自分で作った一時ディレクトリ) に書き、終わりに消している。道具は正しく、手で打つ作法だけが直下に書いている。

## 改善案

### A. 規約: 使い捨てのログは scratchpad (なければ `mktemp -d`) へ。`./tmp` は「セッションを越えて読むもの」だけ (推奨)

`_claude/CLAUDE.md`「一時ファイルの配置」へ追記する (新規 rule は立てない — 発動点が既存の節と同じ「ファイルをどこに書くか」なので)。案:

- **同じ手順の中で書いて読み終えるログ (push / pull / rebase / lint / test の出力) は、ハーネスの scratchpad に置く**。
  scratchpad が無い環境 (別ハーネス・作業者に scratchpad が見えない) では `tmp/<issue番号か作業名>/` に入れる
  (`mktemp -d` は採らない: macOS では `/var/folders` 配下で OS が消すが、消す責任を実装していない点は今の直下と同じ)
- **`./tmp` に置くのは、セッションを越えて人や次のセッションが読むもの** (レポート・サンプル・成果物) と、
  **後から検証する run の出力** (codex / background / detach した長い run の stdout・rc。`_claude/skills/codex-drive/SKILL.md` の
  「run 固有の永続パス」= scratchpad はセッション終了で消え、空振りか遅れて完了かを後から判定できない。obaket 581) だけ。
  置くなら直下に散らさず `tmp/<issue番号か作業名>/` のディレクトリに入れる (`make clean-tmp` がディレクトリ単位で寿命を判定できる)
- worktree の中で走らせる出力の「本体 checkout の `tmp/` を絶対パスで書く」は、上の「`./tmp` に置くもの」に限る旨に直す
- 併せて `verify-execution-not-just-exit-code.md` と `worktree-per-session.md` の `> log 2>&1` の例を、置き場の正本 (`_claude/CLAUDE.md`) を指す形にする
  (例の中にパスを書き足さない — 置き場の規範を 3 か所へ写すと乖離する)

効果: 新しい直下のファイルは「規約に反して書かれたもの」だけになる。scratchpad はセッション専用なので並行セッションの名前の衝突 (`push.log` の上書き) も起きない。
弱点: 規約は破られる。破られても 1 ファイルずつしか増えず、下の B で拾える。

### B. 機械: 直下に増えたファイルを知らせる (A の後に、必要なら)

- 案 B1: Stop hook で「このセッション中に `tmp/` 直下へ増えたファイル」を数え、あれば注入する (消すかディレクトリへ移すかをその場で決めさせる)。
  セッション開始時の直下の一覧を控える必要があり、並行セッションの分と区別できない (自分が書いたと断定できない) のが難点
- 案 B2: `make clean-tmp` に「直下の `*.log` は 3 日」のような短い寿命を足す。自動では走らないので、効くのは人が叩いたときだけ
- どちらも「作ってから掃除する」形 (`adversarial-review-own-safeguards.md` §0-A が先に問えという形) なので、**A で発生源を絞ってからまだ溜まるかを見て決める**

### C. 今ある 1170 件

参照 0 件なので消してよい (ユーザーの判断。`make clean-tmp DAYS=0 DRY_RUN=1` で一覧を見てから)。
🚨 `DAYS=0` はディレクトリ (mutation の worktree 等) も巻き込むので、直下のファイルだけを消すなら
`find tmp -maxdepth 1 -type f \( -name '*.log' -o -name '*.txt' -o -name '*.out' -o -name '*.err' \) -print` で一覧を確かめてから `-delete` に替える。

## 関連ファイル

- `_claude/CLAUDE.md` の「一時ファイルの配置」
- `_claude/skills/codex-drive/SKILL.md` (run 固有の永続パス。A の例外の出典)
- `_claude/issue-rules.d/move-report-conclusions-to-issues.md` (「同じ手順の中で書いて読む出力先」)
- `_claude/rules/verify-execution-not-just-exit-code.md` / `.claude/rules/worktree-per-session.md` (`> log 2>&1` の例)
- `scripts/clean_tmp.sh` / `Makefile` の `clean-tmp`
- `_claude/skills/zundamon-kaisetsu/SKILL.md` (手順 3。サブディレクトリに出す正しい形の例)

## 進捗

- [x] 原因の特定と実測 (本 issue)
- [x] 反証レビュー (sonnet 1 体、read-only): 件数・サイズ・参照 0 件・規約の引用・zundamon の判定は反証されず。
  採った指摘: A が codex-drive の永続パス規約と衝突 → 例外を追記 / `mktemp -d` 案は消す責任を再生産 → 削除 /
  CLAUDE.md に scratchpad の例外が既にある → 原因 2 に追記 / ディレクトリ数 50→51 / prefix・内訳の数え方の幅。
  記録のみ: `_claude/agents/test-runner.md` と `skills/perf-analysis` も `./tmp/` 直下 (`thumbnailthumb-test.log` / `perf.log`) を指すが、
  ThumbnailThumb 専用で 1 ファイル固定名 (上書き) なので溜まらない。A の追記と同時に揃えるかは A の実装時に決める
- [x] A の規約追記 (ユーザー承認 2026-10-15): `_claude/CLAUDE.md`「一時ファイルの配置」に、その場で読み終えるログは scratchpad /
  残すものと後から確かめる run の出力は `tmp/<作業名>/` / 直下に散らさない、を追記。worktree の項も `tmp/<作業名>/` に合わせた。
  `verify-execution-not-just-exit-code.md` と `worktree-per-session.md` の例は `> log` とだけ書きパスを持たないので変えていない
  (置き場の正本は `_claude/CLAUDE.md` の 1 か所)
- [x] C の既存ファイルの削除 (ユーザー承認 2026-10-15): 直前に参照を数え直して 0 件 (1170 個の名前を `grep -rnoF -f` で一括照合。
  必ず当たる `tmp/skill-eval/` で較正して 1 件ヒット)。並行セッションが使用中かもしれない直近 1 時間のファイルは除いて
  `find tmp -maxdepth 1 -type f -mmin +60 -delete` → 1170 → 20 件 (残りは issue 747 の作業中のログ)。ディレクトリ 51 件は触っていない
- [ ] A の後 2 週間ほどで直下の増え方を測り、B が要るかを決める
