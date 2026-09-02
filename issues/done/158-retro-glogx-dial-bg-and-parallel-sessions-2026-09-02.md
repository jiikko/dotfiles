# 158 retro: 盤の背景帯 (実装 → 却下) と、並行セッション運用で踏んだもの 2026-09-02

起票日: 2026-09-02

## このセッションでやったこと

1. ratelimit ダッシュボードの円周を背景色の帯にする (`ffc9d5f`) → 実端末で「色がダサい」で却下、
   `b1d0b73` で revert。記録は `issues/done/157`
2. 経過率・使用率の clamp を `clampPct` へ一本化し、**守っていなかったガード**に回帰テストを付けた
   (`048a510`)
3. `theme/colors.yml` を変えたとき glogx の色 drift 検査が CI で走らない穴を塞いだ (`8b3623a`)

## 反省・気づき (各項目に切り出し先を提案。実行はユーザーの判断待ち)

### 1. ぼやきを 2 回、裏を取らずに出した ← 一番効く

「glogx の theme 色は機械検査の対象外」とぼやき、issue 化まで進めたが**誤り**だった。
`src/glogx/box_test.go` の `TestFrameBorderMatchesThemeYML` が 2026-07-25 から存在し、
`colors.yml` を実行時にパースして突き合わせている。私は `tests/` `docs/` `Makefile` は
grep したが、**`src/glogx/*_test.go` を見なかった**。

- 「Go の検査は `tests/` に無い」= この repo では Go のテストは `src/<proj>/*_test.go` にある。
  検査の所在を「テスト置き場」だけで探したのが誤り
- ぼやきは「判断材料の提供」なので、**誤ったぼやきは誤った判断材料**。しかも軽い口調で出すぶん
  裏取りの敷居が下がる (2 回繰り返して、2 回目でユーザーが issue 化を指示した)
- 救ったのは `issue-creation-codex-review` の**反証レビュー**。「妥当性の確認」ではなく
  「間違っている前提で反証しろ」と投げたので P1 が返った。肯定形で聞いていたら通っていた

**切り出し先案**: `~/.claude/CLAUDE.md`「ぼやきポイント推奨」に一文
(「ぼやきも事実の主張なら裏を取る。取っていないなら『未確認だが』と明示する」)。
新規ルールファイルにするほどではない (既存節の 1 行で足りる)。

### 2. 並行セッションの commit を author と時刻で帰属した

`b6c3b5e` を dotfiles-78 のものと断定して本人に伝えたが、別セッション (既に終了) のものだった。
**全セッションが同じ git user (`jiikko`) なので、author では区別できない**。時刻の近さも、
`git pull --rebase` で他人の commit が自分の commit のあいだに挟まるため根拠にならない
(実際 `cedeeb5 23:30 → b6c3b5e 23:52 → 1e3b6c9 23:47` と時刻が前後している)。

- 帰属が要るときに使えるのは **commit が触ったファイル**と、**本人に聞くこと**だけ
- 誤帰属のコストは「相手に濡れ衣」だけでなく、**真の当事者を探すのをやめてしまう**こと

**切り出し先案**: `_claude/rules/` の並行セッション系
(`parallel-write-agents-need-worktree-isolation.md` か `commit-with-pathspec.md`) に
「commit の帰属を author / 時刻で判定しない」を追記。

### 3. 「やってみて」を見た目の合意と読んだ

`decide-layout-in-sample-renderer-first.md` は「合意の境目はユーザーが**これでいい**と言ったとき」
と定めている。私はサンプル 5 案 + 推しを提示し、「じゃあやってみてほしい」を**推しへの合意**と
解釈して本体実装・テスト・commit まで進めた。ユーザーが実物を見たのはその後で、却下された。

- 「やってみて」は**実装の許可**であって「見た目の承認」ではなかった。ルールは実は破っていない
  (サンプルは回した) が、**サンプルを見たのが私だけ**という穴があった。私は色を判定できない
- 安いのは「実装前に 1 往復」ではなく「**実装を軽くしておく**」だった。実際 revert は
  worktree のおかげで安く済んだので、損失は主にテスト 3 本と検証の時間

**切り出し先案**: ルール改訂 (`decide-layout-in-sample-renderer-first.md` に
「サンプルを**ユーザーが見た**ことを合意の条件にする。モデルが見て選ぶのは合意ではない」)。
⚠️ ただし過剰な確認は「応答・成果物の長さとスコープ」に反するので、**却下が安い場合は
そのまま実装する**という但し書きを付けないとルールが重くなる。要検討。

### 4. rule を足したセッションが `./setup.sh` を忘れて終了すると、無関係な人が踏む ← issue 化候補

`b6c3b5e` が `_claude/rules/avoid-wall-clock-assertions.md` を足したまま `./setup.sh` 未実行で
終了し、`~/.claude/rules/` に link が無い。`tests/claude/test_claude_links_complete.sh` は
正しく fail-closed で落ちる (exit 1 を実測) が、**踏むのは次に `make test` を回した無関係な人**。
当事者は既に居ないので、後始末の持ち主が不在になる。

CLAUDE.md は「足したら `./setup.sh` を再実行する」と規律で縛っているが、**忘れたまま終了することを
止める仕組みが無い**。

**切り出し先案**: 新規 issue。`_claude/` 配下に rule/hook/skill を足す commit で link 漏れを
検出する仕組み (commit 時の hook か、`Stop` hook でのチェック)。⚠️ 設計時に
`adversarial-review-own-safeguards` を通すこと (「検査できなかったときに緑を返さない」形にする)。

### 5. 効いたもの (続ける)

- **worktree 退避**: dotfiles-78 から「同じ `dial.go` を触る」と連絡が来た時点で、私の未コミット
  差分が既に共有ツリーにあった。patch 退避 → worktree → 共有ツリー復元で抜き、相手の pathspec
  commit への混入を防いだ。CLAUDE.md の「並行作業者がいるときの worktree 退避」がそのまま効いた
- **ミューテーション検証**: 背景帯の 3 テストに 5 変異、`clampPct` に 1 変異。特に **clamp を外しても
  既存テストが全部緑**だった発見 (= ガードが無防備) は、変異を当てなければ出なかった
- **描画の A/B バイト比較**: `clampPct` の refactor で「描画が変わらない」を 4 サイズ x 色/mono の
  バイト一致で示した。「テストが緑」より強い証拠になる
- **相手の指摘を鵜呑みにしなかった**: dotfiles-78 の「link 検査は単体実行だと exit 0 (false green)」を
  実測したら exit 1 で、パイプ越しに `$?` を読む罠だった。鵜呑みにしていたら**動いているゲートを
  壊しに行っていた**

## 残課題

- [x] 項目 1 の切り出し → `_claude/CLAUDE.md`「ぼやきポイント推奨」に 1 行
- [x] 項目 2 の切り出し → `_claude/rules/commit-with-pathspec.md`「履歴操作の前に所有者を確認する」に追記
- [x] 項目 3 の切り出し → `_claude/rules/decide-layout-in-sample-renderer-first.md` に 2 行
- [x] 項目 4 の切り出し → issue [160](160-claude-link-leak-after-session-ends.md) を起票
- [x] `./setup.sh` の実行可否 → **不要になっていた** (09/02 08:58 の実行で link 済み。
      `tests/claude/test_claude_links_complete.sh` が rc 0 / 94 個 link 済みを出力)

## 切り出しの内容 (2026-09-02)

### 項目 1 → `_claude/CLAUDE.md`「ぼやきポイント推奨」

「ぼやきも事実の主張なら裏を取る。**不在の主張**(「〜は検査されていない」) は軽い口調ゆえに
裏取りの敷居が下がる。取っていないなら『未確認だが』と明示する」を追記。
実例 (Go の検査は `tests/` ではなく `src/<proj>/*_test.go` にある) も 1 行で入れた。

### 項目 2 → `_claude/rules/commit-with-pathspec.md`

既存の「履歴操作の前に直近コミットの所有者を確認する」節は
「`git log --format='%h %ad %s'` で**自分のものか**確認する」と書いており、
**自分のメッセージとの一致**を見る限りは正しい。そこへ「**他セッションへの帰属には使えない**」を
足した (同一 git user で author では区別できず、`git pull --rebase` で時刻が前後する。
帰属に使えるのは触ったファイルと本人に聞くことだけ)。既存記述と矛盾しない位置に置いた。

### 項目 3 → `_claude/rules/decide-layout-in-sample-renderer-first.md`

「『やってみて』は実装の許可であって見た目の承認ではない。合意の条件は**ユーザーが実物を見たこと**で、
モデルが見て選ぶのは合意ではない」+ 但し書き「**確認を挟むかは却下されたときの損で決める**
(revert が安く実装が軽いなら聞かずに作って見せる方が往復が少ない)」。
retro が懸念していた「ルールが重くなる」は但し書きで回避した。

### 項目 4 → issue 160

`_claude/` への追加を当事者に即時通知する PostToolUse hook を推し (案 A) として起票し、
観点を分けた 2 体の反証レビューを通した。**事実観点は反証 0 件、設計観点が案 A に P1 級の穴を 3 つ**
出したので、推しを **git の post-commit hook (案 E/F)** へ差し替えた。

特に効いた指摘: PostToolUse の matcher `Write|Edit` は Claude Code の Write/Edit ツールしか捕まえず、
**Bash の heredoc / `sed -i` / `git pull` での取り込みは全て素通り**する
(この issue 自身が Bash の heredoc で作られていた = 推しの案が自分の作業経路を検出できない)。
「状態が変わる点は Write ではなく commit」という視点の転換が本体だった。

また、反証レビューが `a7e9b29` (2026-02-09) を掘り当て、「ディレクトリ丸ごと symlink」案が
ubiregi-cli との競合で**却下済み**であることが分かった。issue に「蒸し返さないこと」として明記した。
