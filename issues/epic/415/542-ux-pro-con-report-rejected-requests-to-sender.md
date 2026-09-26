# 542 (ux): 受付の箱で断られた依頼を、置いた人に知らせる (今は pro-con log を見ないと分からない)

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

2026-09-27、外から `pro-con card close C-087 --ending rejected` を打つと、依頼の ID が出て rc=0 で終わった。ところが dispatcher は
「close: レビューの列に無い (今は 質問待ち)」で除けていて (`pro-con log` の kind `reject`)、カードは質問待ちのまま残った。打った側は、log を見るまで気づけなかった。

- `card add` は `--wait` (既定 10 秒) で適用を待ってカードの ID を返す。ほかの操作 (close・rework・answer・delete・order・plan・move 等) は箱に置いたら返る
- 画面から置いた依頼も、除けられたときに画面に出るかは確かめていない

## 期待する動作

- `pro-con card` のどの操作も、既定で適用を待ち、除けられたら理由を出して rc≠0 で終わる (`add` と同じ `--wait`。待てないときは今と同じく依頼の ID と rc=3)
- 画面から置いた依頼が除けられたら、画面に理由を出す (通知)
- PM・PG・取り込みの係の指示書が「rc を見て、除けられたら理由を読む」前提で書かれているかも見直す

## 受け入れ条件

- [ ] 列の合わない close を打つと、理由つきで rc≠0 になる
- [ ] 画面から置いた依頼が除けられると、画面に理由が出る

## 関連

- `src/pro-con/cardcmd.go` (`addAndWait` / `parseCardWait`) / `src/pro-con/store/` (Apply の結果) / 438 (追加オーダー)

## 進捗

(まだ無い)
