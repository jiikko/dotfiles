# 430 (human): claude の bg session がマシンの再起動を越えて戻るかを確かめる

起票日: 2026-09-24
期限: 2026-10-08

親: [415](415-design-claude-pm-worker-orchestration.md) の要件 14 / [425](done/425-research-claude-bg-remaining-measurements.md) から切り出し

## なぜ人が要るか

マシンの再起動は、作業中の他の session や tmux も全部止めるので、Claude から勝手には起こさない。
プロセスが死んだ場合に daemon が自動で再開することは 425 で確認済みで、残るのは OS ごと落ちた場合だけ。

## 手順 (次に再起動するついでに)

1. 再起動の前に、使い捨ての bg session を 1 本立てる:
   `cd ~/dotfiles && env -u TMUX -u TMUX_PANE claude --bg --model haiku -n m430-reboot "Reply with the single word OK. Do not use any tools."`
   (出た短い id を控える)
2. `claude agents --json --all | jq -c '.[] | select(.name=="m430-reboot") | {id,status,state,pid}'` の出力を控える
3. 再起動して、ログインしたら 2 と同じコマンドを打つ
4. 片付け: `claude rm <id>`

## 期待と違ったとき

- 一覧から消えている / `stopped` のまま → 「再起動は越えない」として 426 の「PG が落ちたときの扱い」に書き足し、dispatcher が `--resume` で起こし直す設計にする
- `idle` / `blocked` で戻っている → 越える。415 の要件 14 は Claude Code 側で満たされる

## 進捗

- [ ] 未確認
