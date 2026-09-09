# retro: `issue_done.sh` の新設と truecolor フラグ検出（2026-09-10）

起票日: 2026-09-10
対象セッション: 2026-09-10（別セッションと並行。issue 7 件の消化を依頼された）

依頼: 「dotfiles の作業中の Claude Code セッションがいるので、連絡をとりながら、以下の issue を
開始してください。依頼している issue が被っているかもなので連絡取ってコンフリクトしないように」
→ 追加で「別のセッションと連絡取って open なタスクを効率よく消化してね」。

## やったこと（実測）

- 実装・修正の commit **6 本**（`cd972d27` / `d67b33f3` / `e4ffb9a9` / `81f8c521` / `7241a3aa` /
  `7f619457`）+ この retro の起票 (`1b75c219`) と横展開の修正 (`04a746ac`) で**計 8 本**。
  `done/` へ送った issue **3 件**（334 / 346 / 347）、新規起票 2 件（348 / 349）
- 新設: `scripts/issue_done.sh` + `tests/issues/test_issue_done.sh` /
  `_zshenv.example` + `_dotfiles_check_truecolor` + `tests/zshrc/test_truecolor_flag_check.sh`
- 敵対的レビュー（opus / read-only）1 周。P1 3 件・P2 3 件・P3 5 件
- 変異検証: 347 が 6 本、346 が 5 本（すべて red）

## 気づき

### 1. 🚨 issue の手順の数え落としを、**実測が 1 発で出した**

347 は「done へ移すには 3 つ要る」と書いていたが、**4 つ目**（他ファイルからその issue への
参照の張り直し）が要った。`issues/345` が `](347-….md)` で指しており、3 つだけ実装すると
CI が赤くなる。着手前に `grep -rhoE '\]\((\.\./)*[0-9]{3}-…'` で全数を数えたら即分かった。

→ 切り出し先: **却下**（CLAUDE.md「issue の記述を鵜呑みにしない」「不在の主張は数え直す」が
既に規定しており、守ったら機能した）

### 2. 🚨 **`issues/` の外からの参照は、どの検査も見ていない**

`docs/nvim-ruby-lsp.md:4` が `../issues/332-…md` を指したまま**既に切れていた**（332 を
done へ送ったときの取りこぼし）。2 本のリンク検査は `issues/` しか走査しないので、
**CI は緑のまま壊れる**。issue 293 が「repo 全体を対象にすると議論が広がるので issues/ に閉じた」と
明示的に決めた射程で、その判断自体は妥当。

対応は「検査を広げる」ではなく「**道具が壊さない**」側に倒した（`issue_done.sh` が repo 全体を
走査して張り直し、「移動前のパスを指す参照が 0 件」を事後条件に持つ）。

→ 切り出し先: **却下**（今回の道具で構造的に塞がった。検査の射程を広げるかは 293 の判断のまま）

### 3. 🚨 変異検証が「rollback の実バグ」を出した（テストのバグではなく**製品**のバグ）

`A && { B; C; }` で逆操作を繋いでいたため、`set -e` の下で B（既に在る symlink への `ln`）が
落ちると **C（`git mv` の戻し）に到達せず**、issue が `done/` に置き去りになった。
変異 [no-unclaim] を当てるまで気づかなかった。

→ 切り出し先: **`mutation-verify-new-tests.md` への追記は不要**（既存の「変異を当てて red を
見る」がそのまま機能した例）。ただし
[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)
§1.5 の「段ごとに変異を当てる」が**製品側のバグも出す**ことの実例として価値がある。**判断を仰ぎたい**

### 4. 🚨 敵対レビュー（opus）の P1 3 件は、変異 4 本を全部 red にした**後**に出た

- 追跡外の claim symlink に `git rm` → rc=128 → **rollback に届かない**
  （glogx の `placeNextLink` は `os.Symlink` だけで `git add` しない = **追跡外が通常の状態**）
- Ctrl+C でも同じ窓。しかも EXIT trap が**バックアップごと**消して復旧不能
- `issues/` の外の参照（気づき 2）

`adversarial-review-own-safeguards.md`「変異は自分が想定した不変条件しか試さない」の
**実測 N 件目**。3 件とも自分で再現してから採用した。

→ 切り出し先: **却下**（既存ルールの想定どおり。実測回数を rationale へ足すかは任意）

### 5. 変異が「当たっていない」「構文エラー」の 2 形を両方踏んだ

- 347: リファクタで sed の当て先がずれ、**変異が当たっていないのに green** → 「守られていない」と
  誤読しかけた
- 346: sed の当て方が悪く**構文エラー**の変異を 2 本作った

どちらも `mutation-verify-new-tests.md` の手順 1.5 / 1.6 が名指ししている形。
**手順を読んでいたのに踏んだ**ので、テスト側に機械の guard を入れた
（`diff -q` で当たったことを確認 / `bash -n`・`zsh -n` で構文を確認してから red/green を読む）。

→ 切り出し先: **`mutation-verify-new-tests.md` への追記候補**。「手順 1.5 / 1.6 を**人が覚える**のを
やめ、変異ハーネス側に guard を置く」。発動点は既存項目と同じなので、節を増やさず
手順 1.5 / 1.6 の末尾に 1 行足す形。**判断を仰ぎたい**
（反証レビューで既存ルールとの重複を確認: 手順 1.5 / 1.6 は現状**人が目視で確認する**形しか
書いておらず、`diff -q` / `bash -n` をハーネス自身へ埋める話は無い。**重複ではない**）

### 6. pathspec commit で**連動する生成物**を落とした

`issue_done.sh` が書き換えた `docs/nvim-ruby-lsp.md` / `nvim/ruby-refs-index/README.md` を
pathspec に入れ忘れ、「334 を done へ動かすが inbound 参照は直っていない」**単体で壊れた commit**を
作りかけた（`git rebase` が unstaged で止まって気づいた）。
`commit-with-pathspec.md`「pathspec で生成物を漏らすと壊れたコミットになる」そのもの。

🚨 **道具が repo 全体を書き換えるようになったぶん、pathspec の射程も広がった**のに、
commit のときは「自分が編集したファイル」だけを思い浮かべていた。

→ 切り出し先: **却下**（2026-09-10 の反証レビューで確認）。
[`commit-with-pathspec.md`](../_claude/rules/commit-with-pathspec.md) の
「pathspec で『生成物』を漏らすと壊れたコミットになる」節（`:36-43`）が既に
**「自分が直接編集していない生成物を忘れやすい」「`git status` に残っていたらそれが漏れのサイン」**と
書いており、道具が書き換えたファイルもその「生成物」に含まれる。新規の規範ではない
（**規範は既に在ったのに読まずに踏んだ**、というだけ）。

### 7. 並行セッションとの衝突は「同じファイルの同じ問題」で起きた

私の `scripts/issue_done.sh` が CI の Lint を赤にし、**別セッションが先に直して push**した
（`0a83ba88`）。私も並行して file-level の disable を置いていたので、二重管理になる方を落とした。

損失は小さかったが、原因は**私が push 前に `make test-shellcheck` を回していなかった**こと
（`shellcheck -S warning` は回したが、repo の gate は **info 級**まで見る）。
`verify-execution-not-just-exit-code.md`「個別に直接叩いた結果を集約経路の証拠として使わない」。

→ 切り出し先: **却下**（既存ルールが規定済み。守らなかっただけ）

### 8. 🚨 **散文で書いた `issues/NNN-….md` のパスは、誰も検査していない**

気づき 2 の横展開（「同じ間違いが別の場所にもある前提で grep する」）で repo 全体を数えたら、
**実在する issue を指しているのに `done/` を追っていない参照が 10 件**あった:

| 場所 | 指していた先 |
|---|---|
| `nvim/lua/dotfiles/lsp.lua:38` | 332 |
| `src/lockman/main.go`（2 箇所） | 091 |
| `_claude/rules-rationale/` 3 本 | 226 / 209 |
| `_claude/rules/` 4 本 | 098 ×2 / 100 / 095 |

`issue_done.sh` が直すのは **markdown リンク（`](…)`）だけ**で、これらは
「`` `issues/226-….md` ``」のような**インラインコード内の裸のパス**なので対象外。
`tests/issues/test_issue_links_valid.sh` も `issues/` 配下しか見ない。
つまり **issue を done へ送るたびに、誰にも気づかれず 1 本ずつ増える**。

対応は 3 通りあり、今回は (a) を実行した:

- (a) **今ある 10 件を直す**（実施済み。commit `docs: 散文で書いた issue パスの …`）
- (b) **`issue_done.sh` を裸のパスにも広げる** ← 誤検出が怖い。`issues/NNN-` という文字列は
      テストの fixture（`issues/030-feat-a.md` 等）にも出るので、**書き換えてはいけない対象**が
      同じ形をしている
- (c) **そもそもパスで書くのをやめ、番号だけで参照する**（`issues/README.md` の
      「ファイル名は変えないので `issue 012` で安定して参照できる」が本来の意図）。
      パスを書くから rot する

→ 切り出し先: **新規 issue 候補（(b) か (c) の選択）**。ただし今 10 件を直したので緊急性は無い。
**判断を仰ぎたい**

## 残課題

- [ ] 気づき 5 の切り出し（`mutation-verify-new-tests.md` 手順 1.5 / 1.6 への 1 行追記）の可否
      ← **ユーザーの判断待ち**。気づき 3 は rationale 側の実例候補、気づき 6 は既存節と重複で却下、
      1 / 2 / 4 / 7 は既存ルールが規定済みで却下（残るのは 5 と 8 の 2 件）
- [ ] 気づき 8 の (b) / (c) の選択（散文の issue パスをどう扱うか）← **ユーザーの判断待ち**
- [ ] [issue 349](349-bug-stale-nvim-backup-symlinks-fail-make-test.md)（`make test` が
      環境由来で常に赤）は人の承認待ち

## 追記 2026-09-10: 反証レビュー（read-only）を通した

`issue-creation-codex-review.md` に従い、348 / 349 を「記述は誤りだという前提で反証せよ」で
レビューに掛けた（codex は使わない環境なので read-only サブエージェント）。**P1 は 0 件**。

- **P2**: 気づき 6 は `commit-with-pathspec.md` の既存節と実質重複 → **却下に変更**（上記）
- **P3**: 349 が「CI が赤い」と読めるが**実際は手元だけ**（CI は `setup.sh` を呼ばず、
  検査自身が「未 symlink 環境では素通しで pass する」と書いている）→ 349 の本文を訂正
- **P3**: commit 数の数え方が曖昧（retro 自身の起票を含めるか）→ 内訳を明記
- **P3**: 残課題が気づき 3 を「追記 3 本」に含めていたが、本文の結論は「追記不要」→ 訂正

指摘は 4 件とも**自分で実コマンドを叩いて裏を取ってから**採用した
（`grep -rn setup.sh .github/` = 0 件 / `tests/claude/test_dangling_symlinks.sh:11` の自己申告 /
`_claude/rules/commit-with-pathspec.md:36-43`）。
