# 1078 (feat): yomiyasu の hook を Go で作り直し、ja-textlint・ja-textlint-on-edit・ja-writing に組み込む

起票日: 2026-11-12

## 概要

本家の `yomiyasu_hook.py` (944 行) を `yomiyasu hook` として作り直し、1075〜1077 の Go の実装を dotfiles の文章の検査の入口に組み込む。
ユーザー決定 (2026-11-12): textlint に新しい規則は足さず Go の実装に寄せる。

## 本家の hook が何をするか (v3.1.0)

- `--host claude-code|codex` でホストのイベントの JSON を標準入力から読む。`--print-config` は設定の例を出すだけで、設定ファイルを書き換えない
- 対象は `yomiyasu_check.py start` で登録した原稿だけ。返答の終わり (Stop) に最終稿が検査記録と合うかを確かめ、合わなければ作業を続けさせる。
  登録した原稿に関係する投稿で本文が記録と照合できないときは止める。lint は動かさない。登録していない原稿には何もしない
- イベントごとの時間の上限 (`_BUDGETS`: UserPromptSubmit 4 秒・PreToolUse 7 秒・Stop 20 秒など) と、止める回数の上限を持つ

dotfiles の `_claude/hooks/ja-textlint-on-edit.sh` (Markdown を書いた直後に、書いた行の textlint の指摘を知らせる) とは役割が別で、置き換えにはならない。

## 対応方針

1. **`yomiyasu hook`**: 本家と同じ入出力で作り直す (入力の集まりは、イベントの JSON と状態フォルダの組)。
   dotfiles の `_claude/settings.json` に登録するかは、登録した原稿が無いときの所要 (全イベントで起動する) を測ってからユーザーに聞く
2. **`bin/ja-textlint`**: textlint の後に `yomiyasu lint` を同じファイル (と `--stdin`) に掛け、両方の指摘を出す。rc は「どちらかに指摘があれば 1」を基本に、
   info の扱いを決める
3. **`ja-textlint-on-edit.sh`**: 書いた行に当たった `yomiyasu lint` の指摘も知らせる。知らせる重さ (error・warn・info) と規則は、dotfiles の文書に掛けた当たりを
   抜き取りで読んでから決める (retro 1064 の D: 当たりを読まずに広げない)。時間の上限 (`bin/runtimeout`) の中に収まるかを測る
4. **語の一覧**: dotfiles の git の最上位に `.yomiyasu-words/allow.txt` を置き、dotfiles で定義した用語 (正本・ゲート・台帳・入口・印。ユーザー決定 2026-11-11) を外す。
   当たりを読んで、ほかに定義した用語があれば足す
<!-- textlint-disable ja-ai-vocabulary -->
5. **`ja-ai-vocabulary.js` の整理**: Go の実装の規則 (`metaphor_verb` など) が同じ語を同じ箇所で拾うことを入力で確かめた語から外す。
   dotfiles だけの語 (既定値・既定で・既定の・変異・ソフトウェアの「版」。ユーザー指示) は、本家の `flag.txt` / `flag-patterns.txt` で表せるかを確かめる。
   表せなければ (「版」の除外は後読みを使う) `ja-ai-vocabulary.js` に残す
<!-- textlint-enable ja-ai-vocabulary -->
6. **ja-writing (`_claude/skills/ja-writing/SKILL.md`)**: 否定対比・擬人化・前置きと定型の結び・直訳語の型・直す前後の比較 (`yomiyasu diff`) の規範の文と、
   「ja-textlint の中身」の節を書き直す。SKILL.md を直したら、観点を変えた整合の確認を 3 回と `bin/skill-eval` を回す

## ユーザーに聞くこと (着手のときに)

- `yomiyasu hook` を dotfiles の settings に登録するか (所要の数字を添えて)
- 装飾の記号の規則 (`dash_decoration` など): dotfiles の issues と `_claude` の Markdown 606 本が「—」を使っている (2026-11-12 に `git grep -l` で数えた)。
  書いた行だけを知らせる hook でも、Claude が規範の文書に「—」を書くたびに知らせが出る。知らせるか、規則を外すか

## 受け入れ条件

- [ ] `yomiyasu hook` が入力の集まりで本家と同じ出力と rc を返す
- [ ] `ja-textlint` が textlint と `yomiyasu lint` の両方の指摘を出し、`tests/bin/test_ja_textlint.sh` が両方を見る
- [ ] `ja-textlint-on-edit.sh` が書いた行の `yomiyasu lint` の指摘を知らせ、時間の上限の中で終わる (測った数字を書く)
- [ ] `.yomiyasu-words/allow.txt` で定義した用語が外れる
- [ ] ja-writing の規範と「ja-textlint の中身」が新しい形に合っている

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_hook.py`
- `bin/ja-textlint`・`_claude/hooks/ja-textlint-on-edit.sh`・`_claude/skills/ja-writing/`・`tests/bin/test_ja_textlint.sh`
- 1075 (lint)・1076 (diff)・1077 (check)

## 進捗

- (未着手)
