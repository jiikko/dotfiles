# 616 (bug): 手元の列挙が gitignore された `.claude/worktrees/` まで歩き、偽の赤と余計な lint を出す

起票日: 2026-10-02

## 概要

作業ツリーを `find` で歩く列挙が、gitignore された `.claude/worktrees/`（pro-con が作る worktree。2026-10-02 の手元で 26 個）の中まで数えている。
CI のまっさらな checkout には worktree が無いので、手元でしか起きない。

- `tests/scripts/test_enumerations_are_derived.sh` の ③ が、worktree の中の `_claude/hooks/*.sh` を「LINT_DIRS の外の shell script」として拾い、手元の `make test` で落ちる (偽の赤)。
  2026-10-02 に並列の腕のテストを 1 本ずつ測ったとき、この 1 本だけ rc=1 だった (`✗ LINT_DIRS の外に shell script がある … .claude/worktrees/pc-c-004/_claude/hooks/claude-links-sync.sh …`)
- `scripts/discover_yaml_files.sh` (Makefile の `YAML_FILES`) が、worktree の中の yml を **846 本**拾う (`scripts/discover_yaml_files.sh | grep -c '^\.claude/worktrees'`)。
  手元の `make test-yaml` は、他のセッションの作業中の worktree の yml まで lint する (遅く、他人の途中状態で赤になりうる)
- 同じファイルの ① (実在する yml が導出から漏れていないか) も `find` だが、比べる相手の `YAML_FILES` も同じく worktree を拾うので、今は一致して緑になっている (同じ誤りを両側が持つ)

- `tests/tmux/test_confirm_default_gate.sh` の `find "$ROOT_DIR"` も `.claude` を prune しておらず worktree を歩く。同じファイルを何度も数えるだけで偽の赤にはならないが、遅くなる
- `tests/issues/test_issue_path_refs_not_stale.sh` の `grep -rnI 'issues/' .` も worktree の中まで歩き、手元では 14,431 行を拾って **13 秒**かかる (worktree を除けば 625 行・1 秒未満。
  /usr/bin/grep で実測 2026-10-02)。この検査は pre-push の hook が issue を触る push のたびに回すので、**手元の `git push` が毎回 10 秒以上遅い**。
  hook はスナップショットの中で回すので、CI と hook の中では worktree が無く速い (0.7 秒)。遅いのは手元で直接走らせる `make test` と、hook の外で回したとき

`scripts/discover_shell_scripts.sh` は起点を `LINT_DIRS` の固定のディレクトリにしているので、worktree を拾わない (0 本)。
`scripts/check_ci_group_deps.sh` / `scripts/check_assert_reaches_exit.sh` も起点が固定で拾わない。

## 原因

列挙の母集合を「作業ツリーにあるファイル」で取っていて、「repo が管理するファイル」で取っていない。除外は `.git` / `vendor` / `node_modules` / `tmp` を手で並べているだけで、
`.gitignore` にある `.claude/worktrees/` (`.gitignore` の 24 行目付近) は入っていない。ignore されるディレクトリが増えるたびに同じ穴が開く形。

## 対応方針

- 4 箇所の `find` と `test_issue_path_refs_not_stale.sh` の `grep -r` (`--exclude-dir` に足す) に `.claude/worktrees` の除外を足す: `scripts/discover_yaml_files.sh`、`tests/scripts/test_enumerations_are_derived.sh` の ① と ③、
  `tests/tmux/test_confirm_default_gate.sh` (`-name .claude` で prune すると追跡している `.claude/rules/` まで外れるので、`-path` で worktrees だけを外す)。
  除外の行には理由 (pro-con が作る worktree。中身は別の checkout の写しで、この repo の lint の対象ではない) を書く
- ① は `find` のままにする。導出 (`discover_yaml_files.sh`) と検査が同じ除外を持つ形は今と変わらないが、検査を導出と別の仕組みにすると独立性が増すわけでもない
  (`git ls-files` にすると下の vendor / tmp の問題を持ち込む)
- ignore されるディレクトリが今後増えたときに同じ穴が開く点は残る。再発したら、作業ツリーを歩く列挙を共通の関数に寄せることを考える
- 🚨 採らない案: 母集合を `git ls-files --cached --others --exclude-standard` に寄せて手書きの除外をやめる (反証レビューで崩れた)
  - `vendor/` は追跡されている (138 本、yml も 2 本)。`-not -path '*/vendor/*'` は「ignore 済み」ではなく「第三者のコードを lint しない」除外なので、`git ls-files` に寄せると第三者の yml が lint に入る
  - `tmp/` の ignore は repo の `.gitignore` に無く `~/.gitignore_global` 由来。`--exclude-standard` は手元では tmp を落とすが、新品の checkout や CI では落とさない (手元と CI で挙動が割れる)
- 直したら、手元で worktree がある状態で `make test-yaml` と `tests/scripts/test_enumerations_are_derived.sh` が緑になり、`YAML_FILES` に worktree の yml が入らないことを確かめる

## 関連ファイル

- `scripts/discover_yaml_files.sh` (`found=$(find . …)`)
- `tests/scripts/test_enumerations_are_derived.sh` (① の `actual=$(find . …)`、③ の `allsh=$(find . …)`)
- `tests/tmux/test_confirm_default_gate.sh` (`find "$ROOT_DIR"` の prune の一覧に `.claude` が無い)
- `tests/issues/test_issue_path_refs_not_stale.sh` (`scan_tree` の `grep -rnI 'issues/' .` の `--exclude-dir`)
- `scripts/discover_shell_scripts.sh` (起点が固定のディレクトリなので影響なし。比較用)
- `.gitignore` (`.claude/worktrees/`)

## 進捗

- 2026-10-02 起票 (GHA の rest の内訳の調査中に、手元の計測で見つけた)
- 2026-10-02 反証レビュー (sonnet、読み取りのみ 1 体) の訂正: P1 2 件 (git ls-files に寄せる案は vendor と tmp で壊れる) → 対応方針を差し替え /
  P2 (test_confirm_default_gate.sh の `find "$ROOT_DIR"` も worktree を歩く) → 対象に追加。件数 (846 / 26 / 0)・③ が赤・① が緑の理由・submodule 無しは反証されなかった。
  「CI には worktree が無い」は .claude が追跡されていないこと (`git ls-files .claude` 0 件) からの推論で、CI のログでは未確認
- 2026-10-02 追記: `test_issue_path_refs_not_stale.sh` の `grep -r` も同じ原因で手元 13 秒 (GHA の rest の内訳の調査で、pre-push の hook が回す検査を測って見つけた)
- 2026-10-03 「fix(test): 手元の列挙が .claude/worktrees (pro-con の worktree) を歩かないようにする (616)」 (ユーザーの依頼で着手)
  - 対応方針どおり 4 ファイル 5 箇所: `scripts/discover_yaml_files.sh` と `tests/scripts/test_enumerations_are_derived.sh` の ① ③ は
    `-path ./.claude/worktrees -prune` (中へ降りない。`-not -path` だと歩くこと自体は止まらない)、`tests/tmux/test_confirm_default_gate.sh` は
    prune の一覧に `-path "$ROOT_DIR/.claude/worktrees"` (`-name .claude` だと追跡している .claude/rules まで外れる)、
    `tests/issues/test_issue_path_refs_not_stale.sh` は `--exclude-dir=worktrees` (名前でしか外せない。追跡している worktrees という名前のディレクトリは 0 件)
  - 検証 (A-B): worktree の中に偽の `.claude/worktrees/fake/` (yml・`_claude/hooks/*.sh`・`--default=false` の無い gum confirm・切れた issue のパス) を置き、
    HEAD の版と新しい版を同じ木で走らせた。旧版は 4 つとも worktree の中を拾った (yml の導出 1 本 / 列挙の検査 rc=1 / confirm の検査 rc=1 / issue のパスの検査 rc=1)、
    新しい版は 4 つとも拾わず rc=0。外しすぎの確認: 新しい版の yml の導出 = 旧版の導出から `.claude/worktrees` の行を除いたもの (diff 一致)
  - 手元 (`~/dotfiles`、worktree 26 個) の `grep -rnI 'issues/'`: 7.13 s → 0.15 s (同じ 623 行)
  - worktree での `make test` rc=0 ([ok] 111 本・[FAIL] 0)
  - 敵対的レビューは省略: 検査の対象を狭める除外だけで判定は新設していない。旧版を正解役にして「消えたのは worktree の中だけ」を確かめた (adversarial-review-own-safeguards 0-B)
  - 残り: ignore されるディレクトリが増えたら同じ穴が開く点は対応方針のとおり残す (再発したら列挙を共通の関数へ寄せる)

