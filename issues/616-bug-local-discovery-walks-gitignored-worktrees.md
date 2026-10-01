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

`scripts/discover_shell_scripts.sh` は起点を `LINT_DIRS` の固定のディレクトリにしているので、worktree を拾わない (0 本)。

## 原因

列挙の母集合を「作業ツリーにあるファイル」で取っていて、「repo が管理するファイル」で取っていない。除外は `.git` / `vendor` / `node_modules` / `tmp` を手で並べているだけで、
`.gitignore` にある `.claude/worktrees/` (`.gitignore` の 24 行目付近) は入っていない。ignore されるディレクトリが増えるたびに同じ穴が開く形。

## 対応方針

- 母集合を `git ls-files --cached --others --exclude-standard` (追跡しているもの + 未追跡で ignore されていないもの) で取り、手書きの除外の列挙をやめる。
  未追跡の新規ファイルも拾うので、「足したばかりで add していない yml が lint されない」形は作らない
- 対象: `scripts/discover_yaml_files.sh`、`tests/scripts/test_enumerations_are_derived.sh` の ① と ③。ほかに作業ツリーを `find .` で歩く列挙がないかも、同じ仕組み (`find .` と、除外の手書き) で grep する
- 🚨 ① は「導出が実物の母集合から漏れていない」を見る検査なので、導出と検査の両側を同じ `git ls-files` にすると、同じ誤りを両側が持つ形になる。
  検査の側の母集合を何にするか (例: 検査は `find` のまま、ignore されたパスだけを `git check-ignore` で落とす) を決めてから直す
- 直したら、手元で worktree がある状態で `make test-yaml` と `tests/scripts/test_enumerations_are_derived.sh` が緑になり、`YAML_FILES` に worktree の yml が入らないことを確かめる

## 関連ファイル

- `scripts/discover_yaml_files.sh` (`found=$(find . …)`)
- `tests/scripts/test_enumerations_are_derived.sh` (① の `actual=$(find . …)`、③ の `allsh=$(find . …)`)
- `scripts/discover_shell_scripts.sh` (起点が固定のディレクトリなので影響なし。比較用)
- `.gitignore` (`.claude/worktrees/`)

## 進捗

- 2026-10-02 起票 (GHA の rest の内訳の調査中に、手元の計測で見つけた)
