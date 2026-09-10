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

## 受け入れ条件

- [ ] 「自分の worktree か」を区別する材料を決める。候補: セッション開始時に
      `worktree list` を控えて差分を取る / `$root` の HEAD から到達可能かで絞る（339 の形は
      到達不能なので別途救う）/ **区別せず許容する**（`:19` の宣言を worktree にも広げ、
      dedup に頼る。修正量ゼロで、実害は 1 セッション 1 行）
- [ ] 上の最後の案（許容）を選ぶ場合は、`issue-progress-check.sh:18-19` のヘッダと
      `issues/done/339-*.md` の「検出しないと決めた形」に**誤報方向**を書き足すだけで閉じる
- [ ] コードを直す案を選ぶ場合、339 ② の元の問題（worktree で片付けた issue が毎ターン
      再掲される）を再発させないことを確認する。🚨 **既存の 339 回帰テストでは検出できない**:
      `tests/claude/test_issue_progress_check.sh:152-153` は `check "..." ""` の
      **無出力を期待する否定 assert** なので、primary が空になる（= 何も指摘しない）修正でも緑になる。
      **肯定 assert（自分の worktree の commit なら指摘が出る）を先に足す**
- [ ] 変異検証の制約を 2 つ守る:
      ① **session_id をケースごとに分ける**（`:164-173` が指摘を `$state_dir/$id.reported` に
      署名として書き、2 回目は `:169` で黙る。同一 id で両側を回すと片方が抑制で緑になる。
      既存テストは `s0`…`s10` と分けている）
      ② **fixture の祖先関係を固定する**（他セッション worktree の HEAD が `$base` の
      **子孫**であることが実 repro の条件。非子孫で組むと別ケースを再現する）
