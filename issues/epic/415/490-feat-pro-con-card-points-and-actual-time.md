# 490 (feat): カードの右上に、見積もりの重さ (ポイント) と実際の重さ (作業中だった時間) を数字で出す

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26): 「カードの右上に、カードの重さ (見積もり)・実際の重さみたいな指標を数字で書いて」。
どの数にするかを聞き、ユーザーが選んだ:

- **見積もり = ポイント 1 / 2 / 3 / 5 / 8** (相対の大きさ)。PM がカードを分解してキューに積むとき (`pro-con card plan`) に付ける
- **実際 = 作業中だった時間の合計** (PG が作業中の列に居た時間だけ。質問待ち・レビュー待ち・分解済みで待っていた時間は数えない)
- 作るのは PG (このカード)。見た目は見本を出して人間が選ぶ

## 今の形 (2026-09-26 に読んだ)

- 見積もりはどこにも無い。`card.Card` に欄が無く、`card plan` (`store/store.go` の `case "plan"`) は issue と `--after` だけを受ける
- 作業中だった時間は、履歴 (`Card.History`) の時刻と列の移り変わりから出せるが、欄としては持っていない (`Since` は今の列に入った時刻だけ)
- カードは「タイトル 2 行 + バッジ 1 行」の 3 行 (`ui/view.go` の `cardCell`。ee2f92ba)。右上 = タイトルの 1 行目の右端

## 期待する動作 (未決の点は着手前に質問)

- `pro-con card plan <カード> --issue <repo>#<番号> --points <1|2|3|5|8>`。ほかの値は使い方の誤り。付けないカードは「見積もり無し」(右上に出さない)
  - pm-guide の役目 3 に「積むときにポイントを付ける」と、ポイントの目安 (例: 1 = 数行・テスト 1 本 / 8 = 設計から要る) を書く
- 作業中だった時間: 列が変わるたびに「作業中に居た時間」を足し込む (dispatcher が記録を書くところで。今作業中なら、今の分も足して見せる)。
  🚨 差し戻し・回答で作業中へ戻ったら続きから足す (合計)。落ちて再開を待っている間の数え方は決めて issue に書く
- 表示: カードの 1 行目の右端に「3pt 42分」のような形。狭いときに何を削るか (タイトルを切るか数字を切るか) も決める。
  **見た目は本体に入れる前に ./tmp のサンプルレンダラで候補を並べ、出力を添えて質問し、人間が選んでから入れる** (decide-layout-in-sample-renderer-first)。
  全角と半角の列を混ぜない (no-mixed-width-columns-in-terminal-ui)
- `pro-con card show` と詳細 (enter) にも同じ 2 つの数を出す

## 関連ファイル

- `src/pro-con/card/card.go` (`Card`) / `src/pro-con/store/store.go` (`case "plan"` と列の移り変わり) / `src/pro-con/cardcmd.go` (`plan` の引数)
- `src/pro-con/ui/view.go` の `cardCell` / `src/pro-con/cardview.go` (`card show`) / `src/pro-con/pm-guide.md` の役目 3

## 関連

- 468 (PM の見積もり = `--after`。同じ積む時に付ける) / 469 (カードを開いたら進捗) / 455 (作業中と待ちを分ける = 「作業中の時間」の定義と揃える)

## 順番の見積もり (PM, 2026-09-26。C-047 を C-033 の後に積んだ)

- 触る場所: `card.Card` の欄、`store/store.go` の `case "plan"` と列の移り変わり、`cardcmd.go` の `plan` の引数、`ui/view.go` の `cardCell` のタイトルの 1 行目の右端、`cardview.go`、`pm-guide.md` の役目 3
- 変える判断: 「作業中」に居た時間として何を数えるか
- 順番の理由: 455 (C-033) が「作業中」を、PG が動いている分と待っている分に分ける (枠の数え方は push 済みで、ボードの見た目は人の回答待ち)。同じ定義を 2 つのカードで別々に決めないよう、455 の後にする (依頼の原文でも指定)。452 (C-032) と 485 (C-043) も `cardCell` を触るが、触るのはバッジの行でこちらはタイトルの行なので、順番は付けていない

## 見本の確認のコマンド (C-047 の PG の質問。2026-09-26)

どのディレクトリからでも打てる (フルパス)。見本は PG の worktree の `tmp/` にあり、commit されていない。

```sh
# 色つきで再生する (案 A / B / C と、幅 20 の狭いとき)
python3 /Users/koji/dotfiles/.claude/worktrees/pc-c-047/tmp/pro-con-490-effort-sample.py

# 質問に添えた出力をそのまま見る
cat /Users/koji/dotfiles/.claude/worktrees/pc-c-047/tmp/pro-con-490-effort-sample.ans
```

聞かれていること: 案 A「3pt 42分」/ 案 B「3pt 0:42」(PG の推し) / 案 C「3pt」と「0:42」を 2 段、と数え方の確認 4 点 (カード C-047 の質問。`/Users/koji/dotfiles/bin/pro-con card show C-047`)
