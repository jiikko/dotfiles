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

- [x] 「自分の worktree か」を区別する材料を決める → **worktree ごとの reflog**を使う。
      HEAD を**移しただけ**のエントリ (worktree add = メッセージ空 / `checkout:` / `reset:` /
      `merge <x>:` / `rebase (start): checkout <x>`) を除き、残りを「この worktree で作られた
      commit」とする。実測で語彙を確定させた (`git reflog show HEAD --format='%H%x09%gs'`)
  - 🚨 **列挙するのは「移しただけ」の側**で、「作った」側ではない。未知の verb は「数える」に
        倒れ**今までと同じ挙動**になる。作った側を列挙すると、未知の verb で自分の commit が
        数えられなくなり、339 が直した「毎ターン再掲」へ静かに戻る
  - 🚨 `rebase (start)` を除かないと、rebase 先 (= 他セッションの先端) が「自分の commit」になる
- [x] 339 ② の元の問題を再発させないことを確認する。**先に肯定 assert を足した**
      (`tests/claude/test_issue_progress_check.sh` の「worktree の commit を数えている (肯定)」)。
      既存の否定 assert だけでは、primary が空になる壊れ方でも緑になるため
- [x] 変異検証を 2 本、session_id を分けて実施:
  - **M1** reflog フィルタを外す (353 以前の挙動へ戻す) → 新規の 353 テストだけが red
  - **M2** `commit` を「移しただけ」側へ入れて created_here を空にする → 既存 6 本 +
    **新設の肯定 assert** が red。🚨 このとき**既存の否定 assert は緑のまま**で、
    「否定 assert では検出できない」を実証した
  - どちらも当てる前に `diff -q` で変異が当たったこと、`bash -n` で構文が通ることを確認した
- [x] fixture の祖先関係を固定する → 353 のテストは**現実の経路**で組んだ。他セッションが
      自分の worktree で commit し、**片付けて去った**あと、こちらがその先端で read-only の
      worktree を切る。`$repo` で直接 commit する fixture では再現しない (それは自分の commit と
      区別が付かないのが正しい挙動)
- [x] ヘッダの「検出しないもの」に誤報方向を書き足す。**残る射程**も明記した:
      他セッションの worktree が**まだ存在していて**そこで commit された場合は数える
      (`worktree list` から区別できない。1 セッション 1 回で黙るので実害は 1 行)

## 実装中に踏んだ罠 (記録)

hash の集合を `awk -v` で渡したところ、**BSD awk は `-v` の値に改行を受け付けず**
(`awk: newline in string` で rc≠0) 出力が空になり、**番号が 1 つも出ない = hook が無音**に
なった。既存テスト 6 本が落ちて気づいた。2 入力の `NR==FNR` で渡す形に直した。
`mutation-verify-new-tests.md` が警告している形そのもの。
