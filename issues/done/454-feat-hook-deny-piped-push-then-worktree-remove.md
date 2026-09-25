# 454 (feat): push の結果をパイプに通したまま worktree を消す Bash を、PreToolUse の hook で止める

起票日: 2026-09-25

## 概要

retro 450 の 1 (と retro 266 の同じ形): `git push … | tail -1 && … && git worktree remove …` のように、push の rc をパイプの終端で読んだまま
`&&` で worktree の削除を繋ぐと、push が non-fast-forward で弾かれても後段が走り、未 push の commit を持つ worktree を消す。
規範は既にある (`_claude/rules/verify-execution-not-just-exit-code.md` の「成否で後段を走らせる `&&` のつなぎ」/ `.claude/rules/worktree-per-session.md`) が、
2026-09-05 と 2026-09-25 に同じ形を踏んだ。規範で止まらないので機械で止める (ユーザーの依頼「これもやって」で起票)。

## 詳細

- PreToolUse(Bash) の hook が、コマンド文字列に「`git push` の出力を `|` へ流している」かつ「その後に `&&` で `git worktree remove`
  (と、候補として `git branch -D` / `rm -rf`) が続く」形を見つけたら deny し、正しい形 (`git push … > log 2>&1; rc=$?; [ $rc -eq 0 ] && …`) を案内する
- 🚨 自作の検査 (字句の gate) なので、書く前に `adversarial-review-own-safeguards.md` の §8 に従い、脅威モデルと「検出しない形」をヘッダに書く。
  候補: 守るのは「Claude が 1 本の Bash に push と削除を同居させる」形だけ。変数に入れたコマンド・スクリプトの中・`;` で繋いだ形は検出しない (後者は規範の側)
- 引用符の中・コメント・heredoc の本文 (commit message) は偽陽性になりうる。`deny-bare-tmux-kill.sh` の扱い (引用符とコメントを外す) を参考にする
- テスト: deny すべき形 / 通すべき形 (rc を変数に取ってから繋ぐ・push だけ・パイプ無しの `&&`) を並べ、hook の判定を外す変異で red を見る

## 関連

- retro 450 (切り出し元) / retro 266 / `_claude/hooks/deny-bare-tmux-kill.sh` (同じ字句の PreToolUse の hook)

## 進捗

- 2026-09-25 (dotfiles-7d): 実装した (commit「feat(claude): push の結果をパイプに通したまま worktree / branch を消す Bash を PreToolUse で止める (issue 454)」)。
  _claude/settings.json の PreToolUse(Bash) への配線はユーザーに直接確かめた (dotfiles-5c 経由の依頼だったため)

### 受け入れ

- [x] `_claude/hooks/deny-piped-push-then-destroy.sh`: 脅威モデル・検出しない形・分かっていて受ける偽陽性・fail-closed を冒頭に書いてから判定を書いた (§8)
- [x] deny する形 / 通す形を `tests/claude/test_deny_piped_push_then_destroy.sh` に 70 件で固定 (bash 5 と /bin/bash 3.2 の両方で緑。配線も検査する)
- [x] 変異 (bin/mutate-verify): 1 周目前 12 本 + 直しの各周 (7 / 13 / 5 / 5 / 4 本) がそれぞれ狙った検査で red
- [x] 入口: `.claude/rules/worktree-per-session.md` の「push が成功したことを確認するまで worktree を消さない」に hook の名前を足した

### 敵対的レビュー (Opus) 5 周

- 1 周目: 素通り (push を `( … )` / `{ …; }` で囲むとパイプがグループの外) と、テストの穴 3 件・サイズの上限を文字数で数えていた等を直した
- 2 周目: 素通り 7 本 (1 周目で足した「引用符の中身を残す」経路と ` #` の順序・case の `)` で段が閉じる・`\'` を引用符と読む・`push>log|` 等)。
  **解釈の層を足すたびにその層を騙す迂回が出たので、§8 の「規則の軸が構文にある印」として、シェルの構文を真似る前処理 (引用符・コメント・グループの段) を
  やめ、heredoc の本文だけを落とした字面の上の正規表現 1 本に作り替えた**。代わりに、引用符・コメントの中の同じ並びは deny になる (宣言した偽陽性)
- 3 周目: `git push|tail` (境界が | を食べる)・リダイレクトの & の畳みの漏れ (`2>&3` / `>&-` 等)・改行をまたぐ偽陽性・`--sort -committerdate`・heredoc の O(n²)
- 4 周目: `-vD` / `-dv` / `--del`・push の直後の `;` `&` の偽陽性・/bin/bash 3.2 で heredoc の処理が timeout (awk で入力長に比例する時間へ書き換え)
- 5 周目: `--d` / `--de`・awk の失敗で素通り (fail-closed に)・/bin/bash 3.2 で数 MB の入力が timeout (畳む前に粗い上限)・`<<\EOF`。
  5 周目の直しは判定を新設しない局所的なもので、それぞれ直接の実測と変異で確かめたので、6 周目は回していない (§7 の例外)

### 記録のみ (検出しない / 直していない)

- 冒頭の「検出しない形」: 変数・関数・alias・eval・スクリプト経由 / `rm -rf` での削除 / 別の呼び出しに分けた形 / `pipefail`・`PIPESTATUS[` の誤用 / `"git"` と引用符で書く形
- 算術の `<<n` の後ろに区切り語と同じ語だけの行がある形、1 行に 2 つの heredoc の本文の中にさらに heredoc がある形 (heredoc と読んで間の行を落とす。作為的)
- 🚨 **hook は `~/dotfiles` の実体から動く**。push しても `~/dotfiles` に取り込むまで効かない (本体に他のセッションの未 push の commit があるうちは、本体の持ち主が取り込む)

