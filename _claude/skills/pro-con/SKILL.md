---
name: pro-con
description: pro-con (PM と PG を分けて Claude Code を並列に回すカンバンの TUI) を外から使う・調べるときの入口。カードを積む・PG / PM の質問に答える・止まったカードやレーンを調べる・dispatcher や受付の箱の様子を見る・pro-con で dogfooding するときに読む。「pro-con」「カード」「C-0xx」「PM」「PG」「取り込みの係」「レーン」「dispatcher」「人の番」「dogfooding」で発火。PM / PG / 取り込みの係として起動された session は、渡された指示書 (card guide・依頼の規律) の方に従う。
---

# pro-con を外から動かす

正本は `pro-con help <話題>`。コマンド・状態の置き場はバイナリと一緒に変わるので、ここに写さない。**作業の前に該当の話題を読む**。

| 知りたいこと | 読む |
|---|---|
| 役・レーン・受付の箱・人の番の意味 | `pro-con help terms` |
| カードがどの順にレーンを渡り、誰が何で動かすか | `pro-con help flow` |
| 依頼の出し方・質問への答え方・画面の開き方・削除と片付け | `pro-con help usage` |
| 状態の置き場の中身・止まったとき・クラッシュの後・ライブアップグレード | `pro-con help debug` |
| 実装の事情・設計 | `src/pro-con/README.md` / issue 415 (`issues/epic/415/`) |

## 用語 (1 行ずつ。詳しくは `pro-con help terms`)

- **PM**: 依頼をカードに分けて積み、PG の質問に先に答える。**PG**: カード 1 枚ずつ自分の worktree で実装する
- **取り込みの係**: レビューの列のカードを確かめて master へ取り込む。**テストの係**: PG が頼んだ make test などを順に走らせる
- **見張り**: 取り込みの衝突とテストの順番待ちを見る (読むだけ)。**要約の係**: 失敗の要約と btw の答えを書く haiku
- **dispatcher**: カードの記録の唯一の書き手。受付の箱を適用し、PM / PG / 係を起こす・止める
- **受付の箱**: 画面と `pro-con card` が依頼を置く所。**レーン**: カードの状態 (依頼 → 着手待ち → 作業中 → 質問待ち → レビュー → 完了)
- **人の番**: 人が操作しないと進まないカード (黄の `!人の番`)

## やってはいけないこと

- 状態の置き場 (`cards.json` など) を手で書く・消す。書き手は dispatcher だけ。変えたいことは `pro-con card …` で受付の箱に置く
- 質問待ちのカードを黙って `pro-con card delete` する。質問ごと消え、削除中の間も回答を受け付けない。消す前に人に確かめる
- PG の worktree やブランチを手で消す。`pro-con worktree clean` (一覧) → `--yes` を使う (取り込み済みで誰も居ないものだけ消す)
- 人の番でない質問 (PM が受けている最中) に先回りして答える。答える前に `pro-con card show <カード>` を読む
- 様子を見るためだけに `pro-con` (持ち主の画面) を開いて quit する。最後の持ち主の画面を閉じると dispatcher と PG が止まる。見るだけなら `pro-con --view`
- 頼まれていないのに `pro-con dispatcher` を手で起動する。PG を起動・再開して利用枠を使う (普段は持ち主の画面が起こす)
- `?権限` のカードに `card answer` で答える (受け付けない)。人に画面の `a` で attach してもらう

## 困ったら

`pro-con ps` → `pro-con log --since 30m` → `pro-con card show <カード>` の順に見る (手順の続きは `pro-con help debug`)。
