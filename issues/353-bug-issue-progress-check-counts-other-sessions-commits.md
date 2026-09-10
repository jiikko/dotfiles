# issue-progress-check が他セッションの commit を「自分の作業」に数えて誤報を出す（worktree 経由）

起票日: 2026-09-11
出典: このセッション（dotfiles-82 / issue-sync）で実際に差し戻された
関連: [issue 339](done/339-bug-issue-progress-check-redisplays-explained-findings.md) ② が worktree 列挙を入れた変更

## 症状

`/issue-sync` を回しただけのセッション（issue ファイルを 1 バイトも編集していない）が、
Stop hook に次の指摘で差し戻された。

```
- issues/done/305-bug-tmux-and-go-test-temp-resources-not-swept-at-startup.md:
  変更はあるが、完了チェック ([x]) も進捗 / 結果 / 残タスクの見出しも増えていない
```

305 はこのセッションが触っていない。当時 `~/dotfiles` の HEAD は基準点から動いておらず
（`git rev-parse HEAD` = `e03b95a1`）、`git status --short issues/` も
`git diff HEAD -- issues/` も空だった。

## 原因（実測で確定）

`_claude/hooks/issue-progress-check.sh:55` の `worktrees()` が `git worktree list` の
**全 worktree**を列挙し、`:68-71` の `subjects` が各 worktree で
`git log --format=%s "$base..HEAD"` を取る。

このセッションは read-only の検証用 worktree を **`origin/master` 起点**で切っていた。
基準点は `~/dotfiles` の開始時 HEAD なので、その worktree では基準点より先の commit が見える。

```console
$ cat ~/.cache/claude-issue-progress/<sid>.head
/Users/koji/dotfiles
e03b95a157c3dab1250691b8786a0c1c9f844c57

$ git log --format=%s e03b95a1..432b8ba9      # 432b8ba9 = 検証用 worktree の HEAD
docs(305,351): 本番 tmux を kill した事故を記録する
fix(305): 本番 tmux サーバを kill した経路を塞ぐ (default ガードを kill の前へ)

$ git diff --name-only e03b95a1 432b8ba9 -- issues/
issues/351-retro-issue-305-four-round-adversarial-review-2026-09-11.md
issues/done/305-bug-tmux-and-go-test-temp-resources-not-swept-at-startup.md
```

subject の `(305,351)` が `primary` に入り、`changed` にも `issues/done/305-*.md` が入るので、
構造チェックが「変更はあるが進捗の見出しが増えていない」を出す。**他セッションが push した
commit が、こちらの作業として数えられている。**

## 339 の「安全側」の記述との関係（初版の分析を訂正）

初版はここで「`changed` に余分なパスが入るのは指摘を*減らす*方向で、`subjects` だけが指摘を
*生成する*」と書いた。**これは誤りだった**（反証レビューが指摘、実コードで確認）。

`issue-progress-check.sh:129-142` は `if ! is_changed` の**両分岐で `add` する**:

- `:130`（未変更）… 「このセッションで 1 度も変更されていない」
- `:139`（変更あり）… 「変更はあるが、完了チェック ([x]) も…増えていない」

つまり `changed` に余分なパスが入っても**指摘は消えず、文面が入れ替わるだけ**。抑止が実際に
効くのは関連 open issue の列挙（`:151` の `is_changed "$ro" && continue`）だけで、
339 の「指摘を出さない側」が当てはまるのはその 1 経路に限られる。

**しかも今回出た文面は `:139`**、つまり `is_changed` が true のときだけ到達する行なので、
**339 が「安全」と認定した `changed` 側が作っている**。

339 が worktree のケースに触れていないという初版の記述も不正確だった。
`issues/done/339-*.md:149-152` に「検出しないと決めた形」として書かれている:

> 他セッションの worktree が同じ issue を更新した場合、こちらの「更新漏れ」が抑止される。
> 出さない側なので許容した

ただし書かれているのは**抑止方向だけ**で、**誤報方向（他セッションの commit がこちらの
「作業対象」になる）は書かれていない**。`issue-progress-check.sh:18-19` のヘッダが
`git pull` のケースだけを挙げている、という点は正しい。

## 重要度: P2（既に一部が許容宣言されている）

`issue-progress-check.sh:19` は既にこう書いている:

> `git pull` で混ざった他セッションの commit に付いた番号 (誤報になりうるが、1 セッション 1 回で黙る)

搬送経路（pull か worktree の共有 object db か）は違うが、**failure mode は同一**で、
`:164-173` の dedup により実害は 1 セッション 1 行に留まる。「未知の危険」ではなく
「宣言済みの許容の射程が worktree にも及んでいることが書かれていない」が実体。

初版の「`origin/master` 起点だと**必ず**踏む」も過剰だった。踏むのは 3 つが揃ったときだけ:
①`origin/master` が基準点より進んでいる ②その差分の commit subject に `(NNN)` がある
③対象ファイルの `[x]` / 見出し / cross-ref のカウントが増えていない
（増えていれば `max_over_worktrees`（`:108-116`）が最大値を取るので指摘は出ない）。

## 素朴な修正 3 案はいずれも成立しない（実測で確認）

| 案 | 判定 | 根拠 |
|---|---|---|
| (a) `subjects` は `$root` からだけ取る | **不可** | `primary` は subject 由来と `next/` claim 由来だけで、**変更パス由来の番号は意図的に入れない**（`:73-74` のコメント）。かつ `:87` が `[ -n "$primary" ] \|\| exit 0`。339 が実測した形（cwd = `~/dotfiles` / commit は worktree 側。`issues/done/339-*.md:52,61-62`）では primary が空になり **hook が無音になる** |
| (b) worktree の HEAD が `$base` の子孫かで絞る | **不可** | `git merge-base --is-ancestor e03b95a1 432b8ba9` が **rc=0**。`origin/master` 起点で切れば基準点は必ず祖先なので、報告したケースを 1 件も落とせない |
| (c) `$root` の reflog に無い commit を外す | **不可** | (a) と同じ理由。339 の形（本体 cwd + 自分の worktree の commit）を落とす |

**共通の壁**: 必要なのは「自分の worktree」と「他セッションの worktree」の区別だが、
**祖先関係でも reflog でも両者は区別できない**（どちらも `$root` から見て同じ形をしている）。

## 🚨 1 度目の修正 (`cade38ef`) は revert した — 純粋に悪化していた

worktree ごとの reflog で「その worktree で作られた commit」だけを数える形を実装し、変異 2 本で
red を確認して commit したが、**敵対的レビューが P1 を出し、自分で A-B を取って確認したので
revert した**（`_claude/rules/instrument-before-second-fix.md` /
CLAUDE.md「効果がなかった修正は必ず revert する」）。

### P1: この repo が命じる統合手順で、hook が**恒久的に無音**になる

`.claude/rules/worktree-per-session.md` は統合を
「`git push origin HEAD:master` → `git -C ~/dotfiles pull --rebase` → `git worktree remove --force`」
の 1 セットと定めている。この 3 手を踏むと:

- 自分の commit を記録した reflog は **worktree ごと消える**
- `$root` に残る痕跡は `pull --rebase: Fast-forward` で、除外リストの `pull` に先頭一致する

結果、`$base..HEAD` にその commit が居るのに `created_here` が返さず、`primary` が空 →
`[ -n "$primary" ] || exit 0` で**1 行も出さずに終了**する。しかも一度この窓を通ると、
その issue は**セッション終了までずっと不可視**（単発の race ではない）。

**A-B（自分で再現。使い捨て repo、git 2.43.2 / BSD awk / bash 3.2）**:

| | worktree が在る時点（対照） | push → pull --rebase → worktree remove の後 |
|---|---|---|
| 修正後 (`cade38ef`) | 201 を指摘 | **無出力** |
| 修正前 (`cade38ef^`) | 201 を指摘 | **201 を指摘** |

`$root` の reflog は `pull -q --rebase origin master: Fast-forward` だった。

**交換していたもの**: 「1 セッション 1 行の誤報」を「標準手順に従うと恒久的に黙る」と
交換していた。hook の目的（更新漏れを出口で止める）に対して**純粋に悪化**で、
issue 339 が直した方向の、より悪い形（再掲でなく沈黙）に戻していた。

🚨 **ヘッダに書いた残存リスクは過剰報告の方向だけで、過少報告＝完全な沈黙は 1 行も
書いていなかった**。そして書いていない方が既定経路で発火した。

### P2: `merge --no-ff` は commit を作るのに `merge` として除外される

ヘッダに「`merge <x>:` は移すだけ」と実測として書いたが、これは **fast-forward にしか
当てはまらない**。`--no-ff` の reflog は `merge topic: Merge made by the 'ort' strategy.` で
`^merge` に先頭一致して除外される。`~/dotfiles` の HEAD reflog に実際に 5 件ある。
（対称的に、conflict を解消した merge は `commit (merge):` になるので数えられる。
「衝突した merge は数える / 綺麗に通った merge は数えない」という一貫しない挙動だった）

### P2: 追加した肯定 assert も 1 verb しか踏んでいなかった

ヘッダが 🚨 で禁じた「作った側をホワイトリストにする」変異（`$2 ~ /^commit/`）を当てても、
**新設の肯定 assert を含む全 7 本が緑**だった。fixture が `commit:` という 1 種類の verb しか
通らないため。非等価であることは cherry-pick（別 hash になるよう master を分岐させた形）で
証明されている: baseline は指摘し、変異は無出力。

## 次に試すなら（reflog は材料として不十分）

reflog が壊れているのではなく、**「reflog は自分の commit を記録し続けている」という前提の
射程**が足りない。worktree を消せば記録も消える。材料を変える必要がある:

- **セッション側が自分の worktree を記録する**（`issue-progress-start.sh` が開始時の
  `worktree list` を控え、check 側が「セッション中に増えた worktree」を知る）。
  ただし worktree が消えた後の commit をどう拾うかは別途要る
- **`$base..HEAD` の commit の author date / committer date を、セッション開始時刻と突き合わせる**
  （`issue-progress-start.sh` が時刻も記録する）。他セッションの commit も同時刻帯に来るので
  単独では足りないが、reflog と組み合わせる材料にはなる
- **そもそも直さない**（下記）

## 受け入れ条件

- [x] 素朴な修正候補 3 案が不成立であることを実測で確定させる（表は上記）
- [x] reflog による判別を実装し、**P1 で revert した**。判断の記録を本文に残す
- [x] **肯定 assert は残した**（`tests/claude/test_issue_progress_check.sh` の
      「worktree の commit を数えている (肯定)」）。339 の回帰は「無出力のはず」の否定 assert
      だけで、primary が空になる壊れ方でも緑になっていた。この穴は修正の有無と独立に塞ぐ価値がある
- [ ] **方針の決定**: 次のどちらを取るか
  - (A) **直さない**。`:19` が既に同じ failure mode を許容宣言しており、dedup で実害は
    1 セッション 1 行。ヘッダと 339 の「検出しないと決めた形」に**誤報方向**を書き足して閉じる
  - (B) セッション側が自分の worktree と開始時刻を記録する形で作り直す。**その場合、
    P1 の A-B（push → pull → remove 後も指摘が出ること）を受け入れ条件に入れる**
- [ ] (B) を取るなら、テストの fixture を **`commit:` 以外の作成 verb**
      （`cherry-pick` / `revert` / `am` / `rebase (pick)` / `commit (amend)`）と
      **`pull` / `merge` 経由で `$root` に届く形**（本番の支配的経路。`~/dotfiles` の
      HEAD reflog は pull 202 / commit 107 / merge 84）でも通す

## 参考: revert した実装

`cade38ef fix(353): 他セッションが作った commit を「自分の作業」に数えない` に実装と
変異検証の記録がある。reflog の verb の実測（worktree add = 空 / `checkout:` / `reset:` /
`merge <x>:` / `rebase (start): checkout <x>` は移すだけ、`commit:` / `commit (amend):` /
`rebase (pick):` / `cherry-pick:` / `revert:` / `am:` は作る側）はそのまま使える。
