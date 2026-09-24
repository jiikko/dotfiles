# 426 (design): pro-con の未決の論点を決める

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md) の論点 2〜11

## 概要

本物の PM / PG の backend ([427](427-feat-pro-con-real-pm-pg-backend.md)) を作る前に決めることの一覧。
415 の各論点に叩き台はあるが、以下は決まっていない。実測 ([425](done/425-research-claude-bg-remaining-measurements.md)) の結果で決まるものと、ユーザーが決めるものがある。

## 決めること

- [ ] **カードの置き場所と書き手** (論点 3): PM の Claude がカードのファイルを書き pro-con が読むか、pro-con (daemon) がカードを持ち PM はコマンドで報告するか。叩き台は `~/.local/state/claude-pm/cards/`
- [ ] **振り分けの単位** (論点 6): 受付 PM から担当 PM へ repo / 領域 / 負荷のどれで振るか。担当 PM を dispatcher が自動で増やすか
- [ ] **質問への回答の届け方** (論点 8): 425 の実測で候補が増えた。PG は質問を書いて turn を終え (AskUserQuestion は使わせない。SendMessage で答えられない)、
  idle の PG へ SendMessage で答えを送る形が、`--resume` を使わずに文脈を保てる。これにするかを決める
- [ ] **追加オーダーの届け方** (論点 11): SendMessage は busy の PG にも積めて、turn の区切りで届く (425 結果 3)。これで足りるか、方針変更だけ `stop` → `--resume` にするか
- [ ] **PG が落ちたときの扱い** (論点 5): daemon がプロセスの死を自動で再開する (425 結果 1)。「落ちたら failed にして自動で再実行しない」を捨てて daemon に任せるか、
  再開された PG を watchdog が見張るだけにするか
- [ ] **リソースの宣言** (論点 9): 宣言の書式と置き場所 (project ごとの設定ファイル)、入口を hook にするか PATH の shim にするか、占有中に固まったコマンドを自動で止めるか人間に上げるか
- [ ] **watchdog** (論点 10): 通常の停滞の閾値、TUI を開いていないときの通知

## 関連ファイル

- `src/pro-con/fake/fake.go` (模擬で仮に選んだ振る舞い。決まったら模擬も合わせる)

## 進捗

- [ ] 未着手
