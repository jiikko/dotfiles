# 424 (feat): pro-con の読み取り専用の本物の backend (段階 1)

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

## 受け入れ条件

- [ ] `bin/pro-con` を本物の backend で起動する口 (フラグか config) があり、模擬とは画面で区別できる
- [ ] 実在の session が一覧に出て、状態・最後の発言・cwd が読める
- [ ] 取れないとき (claude が無い / timeout / 読めない transcript) を 0 件として出さない (`agents` の既存の扱いと揃える)

## 関連ファイル

- `src/pro-con/backend/backend.go` / `src/pro-con/agents/agents.go` / `src/pro-con/fake/`

## 進捗

- [ ] 未着手
