# retro: `issue_done.sh` の新設と truecolor フラグ検出（2026-09-10）

起票日: 2026-09-10
対象セッション: 2026-09-10（別セッションと並行。issue 7 件の消化を依頼された）

依頼: 「dotfiles の作業中の Claude Code セッションがいるので、連絡をとりながら、以下の issue を
開始してください。依頼している issue が被っているかもなので連絡取ってコンフリクトしないように」
→ 追加で「別のセッションと連絡取って open なタスクを効率よく消化してね」。

## やったこと（実測）

- commit **6 本**。`done/` へ送った issue **3 件**（334 / 346 / 347）、新規起票 2 件（348 / 349）
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

### 6. pathspec commit で**連動する生成物**を落とした

`issue_done.sh` が書き換えた `docs/nvim-ruby-lsp.md` / `nvim/ruby-refs-index/README.md` を
pathspec に入れ忘れ、「334 を done へ動かすが inbound 参照は直っていない」**単体で壊れた commit**を
作りかけた（`git rebase` が unstaged で止まって気づいた）。
`commit-with-pathspec.md`「pathspec で生成物を漏らすと壊れたコミットになる」そのもの。

🚨 **道具が repo 全体を書き換えるようになったぶん、pathspec の射程も広がった**のに、
commit のときは「自分が編集したファイル」だけを思い浮かべていた。

→ 切り出し先: **`commit-with-pathspec.md` への追記候補**。「**道具が書き換えたファイル**も
自分の変更として数える（`git status` を見てから pathspec を組む）」。**判断を仰ぎたい**

### 7. 並行セッションとの衝突は「同じファイルの同じ問題」で起きた

私の `scripts/issue_done.sh` が CI の Lint を赤にし、**別セッションが先に直して push**した
（`0a83ba88`）。私も並行して file-level の disable を置いていたので、二重管理になる方を落とした。

損失は小さかったが、原因は**私が push 前に `make test-shellcheck` を回していなかった**こと
（`shellcheck -S warning` は回したが、repo の gate は **info 級**まで見る）。
`verify-execution-not-just-exit-code.md`「個別に直接叩いた結果を集約経路の証拠として使わない」。

→ 切り出し先: **却下**（既存ルールが規定済み。守らなかっただけ）

## 残課題

- [ ] 気づき 3 / 5 / 6 の切り出し（既存ルールへの追記 3 本）の可否 ← **ユーザーの判断待ち**
- [ ] [issue 349](349-bug-stale-nvim-backup-symlinks-fail-make-test.md)（`make test` が
      環境由来で常に赤）は人の承認待ち
