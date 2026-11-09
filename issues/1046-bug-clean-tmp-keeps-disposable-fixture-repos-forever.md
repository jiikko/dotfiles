# 1046 (bug): `make clean-tmp` が tmp/ の中の使い捨ての git repo (fixture) を持つ dir を永久に残す

起票日: 2026-11-09

## 概要

`scripts/clean_tmp.sh` (`make clean-tmp`) は、中のどこかに git の印 (`.git`・HEAD と objects/・refs/ が並ぶ git dir) がある
エントリを、古さに関わらず丸ごと残す (issue 729 の設計。登録済みの worktree・bare・`--separate-git-dir` の置き場を消すと、
外の作業ツリーや commit を失うため)。

ところが、セッションがレビュー・変異・計測のために `git init` で作る使い捨ての repo (fixture) も同じ印を持つ。作業ツリーと
git dir の両方が dir の中に閉じていて外から使われないのに、これを 1 つ含むだけで作業 dir ごと永久に残る。

## 実測 (2026-11-09、`~/dotfiles/tmp`)

- `make clean-tmp DAYS=4` で 185 件 / 6.1G を消した後、「中に git の作業ツリーか置き場がある」で残ったのが 23 件 (計 約 0.9G)。
  `dupscan-review-adv1` (19 個)・`974-l1-shadow-2026-10-29` (73 個)・`990-swift-scope-2026-10-30` (51 個) のように、1 つの作業 dir に fixture の repo が何十個も入っている
- `tmp/` の 2 段目より下にある `.git` 232 個の内訳: remote も worktree の登録も無い `.git` dir が 222、origin を持つ clone が 8、
  origin を持ち worktree を 1 つ登録した clone が 1、同じ作業 dir の中の repo を指す `.git` ファイル (fixture の worktree) が 1
- 作ったのは dupscan の道具ではない。`src/dupscan` の実装 (テスト以外) は repo を作らず、`_claude/skills/dupscan/SKILL.md`・`dupscan-dogfood/SKILL.md` にも
  fixture の repo を作る手順は無い。作業 dir の中身 (`r1`〜`r10`・`c1`・`fx/a`・`scale-repo` と、横に並ぶ push / test のログ) から、
  dupscan の開発セッションとそのレビューのサブエージェントが手で作ったもの
- 729 の時点の実測は「`~/dotfiles/tmp` 1000 エントリで印 0 件」で、この形はまだ無かった。729 にはこの形を残す理由の記述が無い

## 対応方針

`find_git` が見つけた印のうち、**次を全部満たす `.git` dir は「dir の中に閉じた使い捨ての repo」として、残す理由に数えない**。
印が 1 つでも当てはまらなければ今までどおり残す (判定できないものは残す側に倒す)。消すかどうかは、その後の今までの規則 (中に DAYS 日以内の更新が無い) で決める。

1. 名前がちょうど `.git` の実体の dir (symlink・`.GIT` などの大文字は今までどおり残す)
2. 親の dir に `.git` 以外のエントリがある (名前が `.git` の `--separate-git-dir` の置き場を外の作業ツリーから使っている形を除く)
3. `config` が実体のファイルで、`[remote`・`[include`・`[includeIf` の節も `worktree =` の行も無い (clone と、外の作業ツリーを指す core.worktree を除く。
   大文字小文字を問わない)。`commondir` も `config.worktree` (extensions.worktreeConfig。core.worktree がそこへ移る) も持たない
3b. 親の作業ファイルが index と食い違わない (`git ls-files -d` が空。fsmonitor は切る)。`git init --separate-git-dir=X/.git W` は X/.git/config に
   core.worktree を書かない (2026-11-09 に実測) ので、X に兄弟のファイルがあると 1〜3 では外の作業ツリーの置き場と見分けられない
3c. tmp の中のどの repo の `objects/info/alternates` もこの git dir を指していない (`clone -s` で借りた側の objects が壊れる)。一覧を読めなければ全部を残す
4. `worktrees/*` が無いか、全部の `worktrees/*/gitdir` の指す作業ツリーが、実体のパスでこのエントリの中にある (指す先を解決できなければ残す)
5. この `.git` dir 自身の HEAD は、この repo の一部として扱う。中の別の git dir (submodule の `modules/*`。origin を持つ clone) は今までどおり残す。
   `.git` ファイルは、`gitdir:` の指す先が実体のパスでこのエントリの中にあれば数えない (指す先の git dir は、それ自身が印として判定される)

bare の形 (HEAD と objects/・refs/ が並ぶが `.git` の中に無い dir) は今までどおり残す (外の worktree が使っているかを中から判定できない)。

### この変更で外れる守り (`list-masked-failure-modes-before-removing-guard.md`)

- 本来の目的 (外の作業ツリー・commit を失わない): 登録済みの worktree (`.git` ファイルが外を指す)・clone (remote あり)・bare・
  `--separate-git-dir` の置き場・core.worktree は 1〜4 で今までどおり残る
- 副次的に守っていたもの: **remote の無い `git init` の repo に、まだどこにも出していない作業がある**場合。DAYS 日 (既定 30 日) 何も書かれていなければ消える。
  tmp/ はスクラッチの置き場で、残す作業は issue・コードへ移す規約 (`_claude/rules/move-report-conclusions-to-issues.md`) なので受け入れる
- 検出しない (中身からは見分けられない): tmp の外の repo が alternates でこの repo の objects を借りている形、外の作業ツリーが `GIT_DIR` で直接この `.git` を指して使う形、
  外の作業ツリーの tracked のファイルがたまたま全部親にも同名である `--separate-git-dir` の置き場
- 厳しすぎて残る形 (受け入れる): 作業ファイルの無い `git init` だけの fixture (親に `.git` しか無い。実データで 9 件。小さい)、clone・bare を含む作業 dir (`877-2026-10-22` など)

### テスト (`tests/scripts/test_clean_tmp.sh`)

- 3 の `r3d` は「clone を残す」と書いて `git init` の repo を作っている (今回消したい形)。本物の clone (origin あり) に直す
- 新しいケース: 使い捨ての repo だけを含む古い dir を消す / 中で閉じた worktree を持つ fixture を消す / 外を指す worktree の登録・remote・include・core.worktree・
  中身が `.git` だけの dir・`.git` ファイルが外を指す形を残す / DAYS 日以内の更新があれば fixture でも残す
- 変異: 条件 1〜5 を 1 つずつ外して red を見る

## 関連ファイル

- `scripts/clean_tmp.sh` の `find_git` / `judge`
- `tests/scripts/test_clean_tmp.sh`
- issue 729 (印で残す設計と敵対的レビュー 4 周の記録)・774 (tmp/ の直下のログ)

## 進捗

- [x] 反証レビュー (sonnet 1 体、読み取りのみ)。採った指摘: P2 `config.worktree` を見ていない (実測で確認) → 3 に足した /
  P2 名前が `.git` の `--separate-git-dir` の置き場に兄弟があると消す (実測で再現) → 3b / P2 tmp の中の別の repo が alternates で借りる形は
  中から検出できる → 3c / P2 テスト 11 の `oldgit/repo` は「中身が .git だけ」で偶然残る・テスト 10 は find_git だけを切り出すので helper を
  検査しない → 直した / P3 件数 (22 → 23・50 → 73 / 51)・相対パスの worktree の登録の解き方。
  却下: 「`worktree=/x` (空白なし) を落とす」は誤り (`[[:space:]]*` は 0 文字にも当たる)。反証されなかった: 件数の内訳・r3d の中身・729 の記述・dupscan は repo を作らない
- [x] 実装とテスト・変異。`tests/scripts/test_clean_tmp.sh` の 14 (fx1〜5 を消す・k1〜13 を残す・新しい fixture は残す・外の作業ツリーと借りた側が壊れない) と 10 (helper ごと ulimit -f 0)。
  変異 16 本がすべて red (条件 2・remote・include・worktree =・commondir・worktree の登録の外・解決できない登録・submodule の置き場を飛ばす・
  使い捨ての dir / ファイルの判定を外す・.git ファイルが外を指す・config.worktree・ls-files -d・alternates・alternates の読み込みの rc・相対パスの基準)。
  緑のまま 1 本: 名前の完全一致の確認を外す (`.GIT`) → その中の HEAD が git dir の形として別に残すので消えない (二重の守りの片方。等価な変異ではない)
- [ ] 敵対的レビュー (opus)
- [ ] `make test`
