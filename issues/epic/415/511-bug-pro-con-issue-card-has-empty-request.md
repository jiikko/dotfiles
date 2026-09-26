# 511 (bug): issue の一覧から足したカードは依頼の原文が空で issue も紐づかず、PM が「何をするカードか」を人に聞き返す

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

画面の issue の一覧 (`i`) から issue を選んで足したカードで、PM が「依頼の原文が空なので、このカードで何をするかを確かめたい」と人に聞いてくる
(C-066: 505 を選んで追加。推奨は「閉じる」)。ユーザーの意図は「issue に書いてあることをそのままやる」で、聞き返しは要らなかった。

## 原因 (2026-09-26、コードで確かめた)

1. **依頼の原文 (`Request`) には人が書いた補足しか入らない**: `live/live.go` の `Apply` の `backend.NewRequest` は
   `Request: c.Text` で、issue を選んで補足を書かなければ空。「issue 〜 に取り組んでください」(`backend.IssuePrompt`) は `Prompt` (PG への指示) の側にだけ入る
2. **足した時点でカードに issue が紐づかない**: `Request.Issues` を積むのは `plan` だけ (`store/store.go` の `case "plan"`)。
   `card show` は「issue: なし」と出す
3. **PM の指示書が原文を基準にしている**: `pm-guide.md` の役目 4「`card show` で依頼の原文…を読む」・「依頼の原文と issue から決まらない…は人間に回す」。
   PM から見えるのは空の原文と「issue: なし」だけで、`Prompt` の中の issue のパスは依頼の本体として読まれない
4. 題名が `#505 (feat): …` になる (`live.go` の `title = fmt.Sprintf("#%03d %s", …)`)。issue 491 の決まり (題名に番号を付けない。番号は紐づけた issue から 1 行目に出る) にこの経路だけ合っていない

同じ入口の別の穴 (同日に踏んだ): `card add --repo /Users/koji/dotfiles` (repo の名前でなくパス) を受け付け、PG の起動で
「repo "/Users/koji/dotfiles" の場所が設定に無い」を 3 秒ごとに出し続けた (`events.jsonl` の kind `launch`)。
PM が `plan --issue dotfiles#510` を付けても、`c.Repo` が空でないので直らない。

## 対応方針

- **模擬 (`fake/fake.go` の `newRequest`) は既にこの形**: 足した時点で `Issues` を積み、原文に「#NNN <題> をやって」+ 補足を入れる。
  本物 (`live`) をこれに揃え、**依頼の原文・題名・紐づける issue を組み立てる所を `backend` の 1 つの関数に寄せて、模擬と本物の両方がそれを呼ぶ**
  (2 か所で同じ組み立てを持つと、今回のように片方だけ食い違う)
- issue から足した依頼は、**足した時点でカードに issue を紐づける** (`store.Request` の add に `Issues` を載せ、`Apply` で `c.Issues` に入れる)。
  epic を選んだときは親 issue を紐づける
- **依頼の原文に意図を入れる**: 補足が無ければ「issue #NNN の本文に書かれていることを進める」、補足があればその後ろに補足。人が書いた文は言い換えない
- 題名は issue 491 に合わせて番号を付けない (`Issue.Title` だけ)。🚨 足した時点で issue を紐づけると `issueTag` の番号も出るので、
  題名に `#505 (feat): ` を残すと二重に出る (`ui/issues.go` の `cardHeading` が落とすのは「数字 + `: `」で始まる形だけ)。模擬の題名も同じく直す
- `pm-guide.md` に 1 項: issue から来た依頼 (カードに issue が紐づいている) は issue の本文が依頼そのもの。残りが確認作業だけでも、それを進める
  (PG に回す / 確かめて書き戻す)。聞くのは本文から決まらないことだけ
- `card add --repo` は、設定に無い repo を受付の箱に置く前に断る (`card plan --issue` の repo も同じ検査を通るか確かめる)。
  箱に手で置かれた依頼も dispatcher の Apply で断る (cardcmd の検査を通らない経路。`CheckPoints` と同じ形)

## 受け入れ条件

- [ ] issue の一覧から補足なしで足したカードの `card show` に、原文 (意図の 1 文) と issue が出る。題名に番号が二重に出ない
- [ ] epic を選んだときも親 issue が紐づく
- [ ] 設定に無い repo の `card add` が rc≠0 で断られ、箱に置かれない。手で箱に置いた同じ依頼も Apply で断られる
- [ ] 新しいテストは変異で red を確かめる (`Issues` を積まない / 原文を空のまま / repo の検査を外す)

## 関連ファイル

- `src/pro-con/live/live.go` (`Apply` の `NewRequest`) / `src/pro-con/backend/backend.go` (`IssuePrompt`・`PMPrompt`)
- `src/pro-con/store/store.go` (add / plan の適用) / `src/pro-con/cardcmd.go` (`card add`) / `src/pro-con/pm-guide.md`
- `src/pro-con/fake/fake.go` (`newRequest`。模擬は既に足した時点で issue を紐づけている = 揃える先)

## 反証レビュー (2026-09-26、sonnet・読み取りのみ)

- 採った: 模擬は既に足した時点で issue を紐づけている → 対応方針を「模擬に揃え、組み立てを 1 つの関数に寄せる」に直した /
  issue を紐づけると題名の番号が `issueTag` と二重に出る → 題名の項に追記
- 反証できなかった: `Request: c.Text` / add は `r.Issues` を読まない / `card show` の「issue: なし」/ pm-guide の役目 4 の文言 /
  `card add --repo` に設定との突き合わせが無い (検査は起動時の `dispatcher.go` だけ) / 491 の決まり / 重なる issue は無い

## 進捗

- 2026-09-26 (C-069): 実装した
  - `backend.IssueRequest` に issue からの依頼の題名 (番号なし)・原文 (「issue #NNN の本文に書かれていることを進める」+ 補足)・紐づける issue を集めた。
    `live.Apply` と模擬 (`fake.newRequest`) が同じものを使う (模擬だけが issue を紐づけ、題名に番号を付けていた二重実装をやめた)
  - `store` の add が `Request.Issues` をカードに載せる。plan / close で同じ issue (repo と番号) を重ねない (`addIssues`)
  - repo の検査の正本は `store.CheckRepo`。`card add --repo` と `card plan --issue` は箱に置く前に rc=2 で断り、
    箱に手で置かれた依頼は `store.Apply` (dispatcher が `d.Repos` を渡す) が除ける。見るのは repo を新しく決めた依頼 (add / plan) だけで、
    前から設定に無い repo のカードへの回答・削除は受ける。`store.Apply` は `repos` を引数に取る (nil なら見ない。テストと設定を持たない呼び手)
  - `pm-guide.md` の役目 1 に「issue から来た依頼は issue の本文が依頼そのもの」を足した
  - 変異 10 件 (issue を載せない / 原文を空 / 題名に番号 / repo の検査を外す / 設定を毎回読む / issue を重ねる 等) がすべて red
