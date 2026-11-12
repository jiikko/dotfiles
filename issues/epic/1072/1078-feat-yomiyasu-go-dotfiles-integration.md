# 1078 (feat): yomiyasu の hook を Go で作り直し、ja-textlint・ja-textlint-on-edit・ja-writing に組み込む

起票日: 2026-11-12

## 概要

本家の `yomiyasu_hook.py` (944 行) を `yomiyasu hook` として作り直し、1075〜1077 の Go の実装を dotfiles の文章の検査の入口に組み込む。
ユーザー決定 (2026-11-12): textlint に新しい規則は足さず、Go の実装に寄せる。

## 本家の hook が何をするか (v3.1.0)

- `--host claude-code|codex` でホストのイベントの JSON を標準入力から読む。`--print-config` は設定の例を出すだけで、設定ファイルを書き換えない
- 対象は `yomiyasu_check.py start` で登録した原稿だけ
  - 返答の終わり (Stop) に、最終稿が検査記録と合うかを確かめ、合わなければ作業を続けさせる
  - 登録した原稿に関係する投稿で、本文が記録と照合できないときは止める
  - lint は動かさない。登録していない原稿には何もしない
- イベントごとの時間の上限 (`_BUDGETS`: UserPromptSubmit 4 秒・PreToolUse 7 秒・Stop 20 秒など) と、止める回数の上限 (2) を持つ

dotfiles の `_claude/hooks/ja-textlint-on-edit.sh` (Markdown を書いた直後に、書いた行の textlint の指摘を知らせる) とは役割が別で、置き換えにはならない。

## 今の ja-textlint の利用者と契約 (変える前に全部を並べ、表をこの issue に書き足す)

- 呼び出し元: `tests/bin/test_ja_textlint.sh`・`tests/claude/test_ja_textlint_on_edit.sh`・`_claude/hooks/ja-textlint-on-edit.sh`・
  `_claude/skills/ja-writing/SKILL.md`・`mac/README.md`・`_claude/CLAUDE.md` の skill の表・どの repo からも PATH で呼ぶ人と Claude
- 契約
  - 残りの引数は textlint にそのまま渡り、`--fix` も使える
  - rc は textlint のまま (0 / 1 / 2) で、依存が無いときは 3
  - `ja-ai-vocabulary` の warning だけなら rc 0 (テストが契約にしている)
- hook の時間の上限: textlint を `bin/runtimeout 10` で止め、hook の全体は 15 秒。指摘の有無の判定に使えない終わり方 (rc 2 / 3・時間切れ) のときは、何も知らせずに通す

## 対応方針

1. **`yomiyasu hook`**: 本家と同じ入出力で作り直す (入力の集まりは、イベントの JSON と状態フォルダの組)。
   dotfiles の `_claude/settings.json` に登録するかは、登録した原稿が無いときの所要 (全イベントで起動する) を測ってからユーザーに聞く。
   登録しないと決まった場合は、その決定と理由をこの issue に書いて、この項目を閉じる
2. **`bin/ja-textlint`**: textlint の後に `yomiyasu lint` を同じファイル (と `--stdin`) に掛け、両方の指摘を出す
   - textlint だけに渡す引数 (`--fix` など) の扱いを決める (`yomiyasu lint` は受けない)
   - rc の合成の表を作る。今の契約 (warning と info だけなら rc 0) を保つのを基本にする。片方が rc 2 / 3 やビルドの失敗で終わっても、もう片方の指摘は捨てない
   - 決めた表に合わせて、上の呼び出し元のテストと文書を同じ変更で直す
3. **`ja-textlint-on-edit.sh`**: 書いた行に当たった `yomiyasu lint` の指摘も知らせる
   - 知らせる重さ (error・warn・info) と規則は、dotfiles の文書に掛けた当たりを抜き取りで読んでから決める (retro 1064 の D: 当たりを読まずに広げない)
   - 知らせない規則は dotfiles の側 (`--json` の出力の jq の絞り込み) で外す。`yomiyasu lint` に規則を外す引数を足すと本家の答えと揃わなくなるので、足さない
   - Go のビルドを hook の時間に入れない。hook からは `go_autobuild_exec --no-build` で起こし、ビルドが無ければ知らせずに通す (npm の `JA_TEXTLINT_NO_INSTALL` と同じ扱い)。
     `--async` は古いバイナリの結果を今の結果と取り違えるので使わない
   - 時間の上限の中に収まるかを、ビルド済みで cache が冷えた状態と温まった状態の両方で測る
4. **語の一覧**: dotfiles で定義した用語 (正本・ゲート・台帳・入口・印。ユーザー決定 2026-11-11) を外す一覧を dotfiles に置く
   - どの repo から `ja-textlint` を呼んでも同じ用語が外れるように、`yomiyasu lint` に `--words` で dotfiles の一覧を渡す。repo が自分の `.yomiyasu-words/` を持つときにどちらを使うかを決める
     (本家の `--words` はフォルダを 1 つだけ受ける)
   - 当たりを読んで、ほかに定義した用語があれば足す
5. **dotfiles の textlint の規則の整理**
<!-- textlint-disable ja-ai-vocabulary -->
   - `ja-ai-vocabulary.js`: Go の実装の規則 (`metaphor_verb` など) が同じ語を同じ箇所で拾うことを入力で確かめた語から外す
   - dotfiles だけの語 (既定値・既定で・既定の・変異・ソフトウェアの「版」。ユーザー指示) は、本家の `flag.txt` / `flag-patterns.txt` で表せるかを確かめる。
     `flag-patterns.txt` は先読み・後読みを受けるが、上限の無い `*` `+` と `\p{...}` を断り、Python の後読みは固定幅だけを受ける。今の「版」の除外がこの範囲に収まらなければ `ja-ai-vocabulary.js` に残す
   - `ja-bold-not-rendered.js`: 本家の `bold_not_rendered` (error) から着想した規則で、on-edit の hook が知らせる 2 規則のもう一方。
     Go の実装の `bold_not_rendered` が同じ箇所を拾うことを確かめてから外し、hook の jq の絞り込みも直す
<!-- textlint-enable ja-ai-vocabulary -->
6. **ja-writing (`_claude/skills/ja-writing/SKILL.md`)**
   - 否定対比・擬人化・前置きと定型の結び・直訳語の型・直す前後の比較 (`yomiyasu diff`) の規範の文を足す
   - 「ja-textlint の中身」の節を書き直す
   - SKILL.md を直したら、観点を変えた整合の確認を 3 回と `bin/skill-eval` を回す
   - 2026-11-12 の照合の要点 76 項目のうち、Go の lint / diff でも ja-writing の文でも扱わないもの (本家の SKILL.md の推敲の手順・読み手の段階・事実を補う手順など、
     コードでない規範) の行き先を、項目ごとに「ja-writing に足す / 足さない (理由)」で決めて書く

## ユーザーに聞くこと (着手のときに)

- `yomiyasu hook` を dotfiles の settings に登録するか (所要の数字を添えて)
- 装飾の記号の規則 (`dash_decoration` などの 3 本。warn なので `--strict` の判定にも入る): dotfiles の issues と `_claude` の Markdown 606 本が「—」を使っている
  (2026-11-12 に `git grep -l` で数えた)。書いた行だけを知らせる hook でも、Claude が規範の文書に「—」を書くたびに知らせが出る。知らせるか、外すか

## 受け入れ条件

- [ ] `yomiyasu hook` が入力の集まりで本家と同じ出力と rc を返す
- [ ] `ja-textlint` が textlint と `yomiyasu lint` の両方の指摘を出す。rc の合成の表のとおりに、`tests/bin/test_ja_textlint.sh` が両方を見る
- [ ] `ja-textlint-on-edit.sh` が書いた行の `yomiyasu lint` の指摘を知らせる。ビルドが無いとき・時間切れのときに知らせずに通ることもテストで見る。測った所要の数字を書く
- [ ] dotfiles で定義した用語 5 つを、`yomiyasu lint` の全規則 (組み込みの一覧を含む) に通して当たりが 0 件になる。dotfiles の外の repo から呼んでも 0 件になる
- [ ] ja-writing の規範と「ja-textlint の中身」が新しい形に合っている
- [ ] CI の Tests の各ジョブで、`test_ja_textlint.sh` が `yomiyasu lint` を飛ばして緑になっていない (Go の要るジョブで回っていることを、ログの行で確かめる)

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_hook.py`
- `bin/ja-textlint`・`_claude/hooks/ja-textlint-on-edit.sh`・`_claude/skills/ja-writing/`・`tests/bin/test_ja_textlint.sh`・`tests/claude/test_ja_textlint_on_edit.sh`
- 1075 (lint)・1076 (diff)・1077 (check)

## 進捗

- (未着手)
