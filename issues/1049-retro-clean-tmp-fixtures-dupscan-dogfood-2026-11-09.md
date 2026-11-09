# 1049 (retro): clean-tmp の使い捨て repo・dupscan-dogfood (treefiler / tuikit) のセッション — 2026-11-09

起票日: 2026-11-09

## 概要

次の順で進めたセッションの振り返り。
1. retro 1038 と issue 774 を done にし、`tmp/` を片付けた (7.2G → 22M)
2. `make clean-tmp` が使い捨ての git repo を永久に残す件を、issue 1046 で直した (敵対的レビューを 4 周)
3. dupscan-dogfood で treefiler / tuikit の重複を寄せ、dupscan の所見 4 件を直した (issue 1047)

## どこで踏んだか

1. **安全機構の敵対的レビューが 4 周かかり、1〜3 周目は毎回「消してはいけないものを消す」形を再現された**
   - 3 周とも、壊せた所は「git が持つ状態を、ファイルの字面で読む」ところだった (config を grep・alternates のファイルを find で探す・HEAD を改行で割って列挙)。
   - 直し方は毎回「git 自身に答えさせる」(`git config --file`・`count-objects -v`) か `-print0` だった。
   - `rules-checklists/adversarial-review-own-safeguards.md` の §0-B (答えを既に出している経路はないか) と §8 (軸が構文にある) を、書き始める前に読んでいなかった。retro 1038 の項目 1 と同じ形。
2. **長い検証を Bash ツール 1 回の中の素の `&` で背景に回し、走っている間に同じ worktree を編集した**
   - `make -C src/dupscan lint test` を `( … ) &` で起こした。完了の通知が来ず、その間に編集したので、run は古い版を検証していた。pid を `ps` で探して止め、回し直した。
   - 規範は `verify-execution-not-just-exit-code.md` にある (retro 1030 で足した)。
3. **python の書き換えと `git commit` を、`;` と改行でつないだ 1 本の Bash に入れ、置換が外れたまま commit した (2 回)**
   - python の assert で書き換えは止まったが、後ろの commit が走り、issue の本文が古いまま commit に入った。どちらも気づいて amend か別の commit で直した。
4. **寄せない理由を確かめずに書いた**
   - issue 1047 に「childContext は、寄せると tuikit にテストだけの依存が増える」と書いた。実際は tuikit は 2026-10-30 から testwait を取り込んでいて、並行セッションに指摘された。
5. **敵対的レビューのエージェントに既定の `mktemp -d` を使わせ、OS の一時領域の直下に dir を 5 つ残した**
   - そこは hook が直接の片付けを止め、`tmpclean` も日数 (既定 3 日・最小 1 日) がたつまで候補にしないので、その日のうちに片付けられなかった。
   - 4 周目だけは置き場を本体 checkout の `tmp/1046-2026-11-09/` の中に指定し、そこはエージェントが自分で片付けられた。
6. bash 3.2 で、変数の直後の全角の句点が変数名の続きに読まれた (テストの実行で見つかった)。

## 提案

- 却下 (1): 規範は §0-B と §8 に既にある。踏んだのは、発動したのに書き始める前に checklist を読まなかったため。新しい規範は足さない
- 新規 issue (2): **1050**。長い検証を素の `&` で背景に回す形を PreToolUse(Bash) の hook で止める。rule の追記で止まらず 2 回目なので、機械で止める
- 却下 (3): `verify-execution-not-just-exit-code.md` の「検証の手順は `&&` で繋ぎ、`;` で繋がない」が当たる (python の assert で書き換えは止まっていたので、後ろの commit を `&&` で繋いでいれば走らなかった)
- 却下 (4): `_claude/skills/dupscan-dogfood/SKILL.md` の 3 節の「寄せないと決めて理由を書くときは、書く前に実コードで確かめる」と、`claude-md-maintenance.md` の「根拠を引くときはその場で読む」が当たる
- `_claude/rules/` へ追記 (5): `subagent-model-tiering.md` の「指示を書くとき」に、「作業者に実験の dir を作らせるなら、置き場を本体 checkout の `tmp/<作業名>-<日付>/` の中に指定する」を足した。
  新しい rule ではなく既存の節への追記 (発動点は「委ねる指示を書くとき」で同じ)。OS の一時領域に作らせた残骸は、同じ日のうちには道具で片付けられないことが新しい事実
- 却下 (6): issue 633 の lint (`make test-lint`) が既に検出する。今回は lint に届く前にテストの実行で見つかった

反証レビュー (sonnet 1 体、読み取りのみ): 事実の誤りは無し。採った指摘: 1050 に serial-run-wrap との順番・規範が認める detach (nohup / setsid / disown) を通すこと・判定の単位を足した / rule の追記を「一時ファイルの配置」を指す形に縮め、tmpclean の日数を既定 3 日・最小 1 日に直した / 却下 (3) の引用を 1 つに絞った。

残課題は無い (全項目が issue 化・rule への追記・却下で決着)。
