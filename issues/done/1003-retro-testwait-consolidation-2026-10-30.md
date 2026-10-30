# 1003 (retro): Go のテストの待ちを testwait に寄せたセッションの振り返り

起票日: 2026-10-30

## 概要

dupscan の文書の更新 (acf6f526d・f3b40a9e6・a335a9c42)・サブコマンドの見直し (cd416ff94)・treefiler の CI の flaky の観測 (c3d3ce4a2) から、
Go のテストの待ちを共通の module `src/testwait` に寄せる (b210622e7・e23b45849) までのセッションで踏んだこと。

## 踏んだこと

### 1. 共有の本体で、worktree 用の書き換えスクリプトを走らせた

workflow の `paths` を足すスクリプト (相対パスで `.github/workflows/src_<module>.yml` を書き換える) を、前のコマンドの
`cd /Users/koji/dotfiles && …` の行に続けて打ち、本体の 6 ファイルを書き換えた。同じ呼び出しの後半で worktree でも
走らせたので、出力の行数の違いで気づいた。差分が自分の 12 行だけと確かめて本体を戻した。
相対パスで書くスクリプトの作業根は「その行の cd」で決まり、どの checkout を書いているかが呼び出しの字面に出ない。

- 提案: **ファイルを書き換えるスクリプトは、作業根を引数 (絶対パス) で受け取る形で書き、cd と相対パスに頼らない**。
  切り出し先: `.claude/rules/worktree-per-session.md` に追記

### 2. rebase が止まったのに「push 済み」の確認が通り、worktree を消した

push の再試行のループで rebase が衝突して止まり (別のセッションが同じ直しを先に push していた)、続く確認
`git merge-base --is-ancestor $(git rev-parse HEAD) origin/master` が真になって `worktree remove --force` まで進んだ。
止まった rebase の HEAD は最後に当て終えた commit を指す。今回は自分の commit が 1 つで、その最初の commit で止まったので
HEAD は origin の側の commit を指し、この確認は真になった (前の commit が当たっていれば偽になる)。消えたのは同じ中身の自分の commit だけで、
実害は無かった。

- 提案: **push を確かめるときは、HEAD ではなく自分の commit (subject か patch-id) が origin にあるかを見る**。rebase の rc を見て
  止まったら抜けるのは `commit-with-pathspec.md` の push の再試行の項に既にあるので、そちらを指す。
  切り出し先: `.claude/rules/worktree-per-session.md` の「push が成功したことを確認するまで worktree を消さない」に追記

### 3. 機械的な書き換えで、失敗の分岐の中で遅れて評価していた式を、待ち始めに評価する形にした

`case <-time.After(d): t.Fatalf("…%s", r.Text())` を `testwait.Recv(t, fmt.Sprintf("…%s", r.Text()), …)` に書き換えると、
出力の読み取りが時間切れのときから待ち始めへ移る (待つ前の出力を出し、別の goroutine が書く変数を読んで競合しうる)。
最初の書き換えで 27 か所がこの形になり (grep で数えた途中の数)、書き換えの見本を目で見て気づいた。`Diag` の関数の中で作る形に直して
当て直した (最終的に e23b45849 で `testwait.Diag` を使うのは 37 か所。lint の直しで連結に変えた所と、時間切れの前の文を移した所を含む)。

- 提案: **分岐の中の式 (失敗のときだけ作る文・後片付け) を関数の引数へ移す書き換えでは、評価の時点と回数が変わらないかを
  突き合わせる。変わるなら関数の中 (遅れて呼ぶ形) に包む**。
  切り出し先: `_claude/rules/survey-receiver-guards-before-passing-new-values.md` の「逆向き」の下の、共通の道具に寄せるときの項の並びに追記

### 4. 他人の直近の commit で赤くなった CI を、作者と同時に直した

unused の赤 (a84449964 が残した 1 行の包み) を直して commit したところ、作者の側が同じ直し (1e109b92f) を先に push していた。
申告をしないまま「小さい直しだから」と書き始めた。

- 却下: `parallel-write-agents-need-worktree-isolation.md` の「書き始める前に、触る予定のファイルと関数を並行セッション全員へ申告する」で
  足りる (守れなかっただけ)。「作者に聞く」は、author が全セッション同じで作者のセッションを特定できないという同じ項の記述と矛盾する

### 5. 却下・局所

- 却下: `make test` を背景の待ちに乗せず `&` で起こし、結果のファイルを別の待ちで待った。`verify-execution-not-just-exit-code.md` の
  「ツールの実行時間上限を超えうるコマンドは、ツールが実行を維持する手段を使う」で足りる (守れなかっただけ)
- 却下: lint の指摘を 1 周目は報告された行だけ直し、2 周目で同じ形の指摘が出た。CLAUDE.md の「直したバグは同じ間違いが
  別の場所にもある前提で探す」で足りる
- 局所: zsh で `for d in */; do d=${d%/}` が効かず、ログの置き場所が壊れた。bash に切り替えて済ませた (直さない)

## 反証レビュー (opus・read-only、2026-10-30)

指摘 5 件をすべて採った: 3 の件数 (27 は途中の数で、最終の Diag は 37) を書き分けた / 4 の提案は既存の申告の項と重なり、
作者の特定を前提にする点で矛盾するので却下に回した / 2 の「必ず真」を「最初の commit で止まると真」に直し、提案を自分の commit での照合に
絞った / 3 の切り出し先を、共通の道具に寄せるときの項の並びにした。commit の hash と主題・6 ファイル 12 行・却下の根拠の実在は反証されなかった。

## 進捗

- [x] 1 → `.claude/rules/worktree-per-session.md` に追記
- [x] 2 → `.claude/rules/worktree-per-session.md` の「push が成功したことを確認するまで worktree を消さない」の下に追記
- [x] 3 → `_claude/rules/survey-receiver-guards-before-passing-new-values.md` の共通の道具に寄せるときの項の並びに追記
- 4・5 は却下 (理由は各節)
