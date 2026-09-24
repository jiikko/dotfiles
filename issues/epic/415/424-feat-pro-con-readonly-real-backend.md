# 424 (feat): pro-con の読み取り専用の本物の backend (段階 1)

> 🚨 **担当中: dotfiles-5c**（2026-09-24〜）

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md) の「段階」1

## 概要

今の pro-con は模擬の backend (`src/pro-con/fake`) でしか動かない。最初の本物のつなぎ込みとして、
**書き込みの無い** backend を作る: 今の Desktop の対話 session を、担当 issue・最後の発言・状態つきのカードとして並べる。
PM / PG の仕組みが無くても「どの session が何をしているか忘れる」(415 の背景) はこれで大半が消える。

## 詳細

- 情報源は `claude agents --json` (session の一覧と status。`src/pro-con/agents` が読める) と、`sessionId` から引ける transcript
  (`~/.claude/projects/*/<sessionId>.jsonl`)
- `backend.Backend` の `Poll` / `Snapshot` を実装し、`Apply` は `ErrUnknownKind` 系で拒否する (書き込みを持たない)。`AttachCommand` は `claude attach` を返してよい
- カードの対応: session 1 本 = カード 1 枚 (人間の依頼の単位ではないので、415 の不変条件「カード = 依頼 1 件」とは別扱いになる。どう見せるかを決める)
- 担当 issue の推定: session の cwd と transcript から読める範囲 (`issues/next/` の claim・commit message の issue 番号など)。推測で埋めず、読めなければ「不明」

## 未実測 (着手時に最初に測る)

- Desktop の対話 session と `--bg` の session が同じ形式で transcript を書くか
- `claude agents --json` の `status` が idle / busy 以外の値 (waiting) を対話 session で返すか

## 模擬 (ハリボテ) と本物の併用 (2026-09-24 の要望「動作確認のためにハリボテを使いたい」)

- **起動するときに選ぶ**。例 `bin/pro-con --mock` で模擬 (`src/pro-con/fake`)、付けなければ本物。模擬は本物ができた後も捨てず、
  動作確認と自動テスト (ui のテストは模擬の上で動いている) に使い続ける
- **どちらで動いているかをヘッダーで区別する** (今の「mock: claude は起動しない」の表示を、本物のときは出さない / 本物と分かる表示にする)
- **状態ファイル (カードの記録・ライブアップグレードの引き継ぎ) は模擬と本物で別の場所に置く**。模擬のカードが本物の記録に混ざらないように
- **1 つの画面に模擬と本物のカードを混ぜない**。見えているカードが本物の PG か模擬かを取り違えると、本物の PG に回答や方針変更を送る事故になる

## 受け入れ条件

- [x] `bin/pro-con` を本物の backend で起動する口 (フラグか config) があり、模擬とは画面で区別できる (上の「模擬と本物の併用」節)
- [x] 模擬と本物で状態ファイルの置き場所が分かれている
- [x] 実在の session が一覧に出て、状態・最後の発言・cwd が読める (隔離 tmux で 6 本を確認)
- [x] 取れないとき (claude が無い / timeout / 読めない transcript) を 0 件として出さない (前のカードを残し、理由を不変条件の違反の欄に出す)

## 関連ファイル

- `src/pro-con/backend/backend.go` / `src/pro-con/agents/agents.go` / `src/pro-con/fake/`

## 進捗

- [x] 読み取り専用の本物の backend (`src/pro-con/live`) と `--mock` の切り替え (2026-09-24)
  - 実測 (Claude Code 2.1.281): Desktop の対話 session と `--bg` の session の transcript は同じ形式 (bg にだけ `sessionKind`・`custom-title`・`agent-name`)。
    題名は `ai-title` / `custom-title`、最後に人間が打った文は `last-prompt`、人間の発言は `origin.kind == "human"`。
    14MB の transcript の末尾 512KB を 5ms で読める。bg の最初の依頼は人間の発言の印が付かない (依頼の原文は last-prompt から取る)
  - `claude agents --json` は 1 回 0.15 秒かかるので、画面の tick では呼ばず、裏の goroutine が 3 秒ごとに読み直す
  - 担当 issue は推測しない (今は空)。対話の session の状態は idle / busy しか観測していない (waiting は bg だけで確認)
