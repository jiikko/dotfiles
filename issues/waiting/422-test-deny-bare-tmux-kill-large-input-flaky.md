# 422 (test): test_deny_bare_tmux_kill.sh の「大きな入力でも deny」が 1 度だけ allow になった

起票日: 2026-09-24

> **waiting: 再発を待つ。** 次に落ちたとき、`✗ 大きな入力 (90KB) でも deny を出す` の直下の行が
> `実際=timeout (N 秒)` なら負荷による時間切れで確定 (下の仮説どおり)。`実際=allow` なら判定ロジックか
> hook の早期 return で、仮説が外れている。どちらかが出たら open に戻して対応方針を決める

## 概要

`tests/claude/test_deny_bare_tmux_kill.sh` の「大きな入力 (90KB) でも deny を出す」ケースが、
2026-09-24 の `make test` で 1 度だけ allow になった。その後は再現していない。

## 詳細

- 判定ロジックは入力に対して決定的なので、同じ入力で 1 度だけ allow になるのは「hook が無出力で終わった」形しかない。
  テストの `decision` は無出力を rc で振り分けていて、**rc=0 (何も deny しなかった) と rc=124 (timeout に殺された) を
  どちらも allow に畳んでいた**。そのため落ちたときの出力からは原因を区別できなかった
- 実測 (2026-09-24, 14 コア): 90KB の入力で hook の所要時間は単独 1.4 秒、`yes` を 42 本 (CPU 3 倍) 走らせた下で 5〜6 秒。
  テストが課す timeout は本番と同じ 10 秒 (`_claude/settings.json` の PreToolUse)。`make test` は並列の腕と直列の腕を
  同時に走らせ、Go の `-race` も同居するので、10 秒を超える負荷は起こりうる (**仮説。落ちた回の rc は残っていない**)
- 本番の意味でも、負荷で 10 秒を超えれば deny は出ない。ただし本番で 90KB のコマンドはまず来ない

## 対応方針

- `decision` が rc=124 を `timeout` として別に返すようにし、失敗時に所要秒数と切り分けの案内を出す
  (次の再発で原因が確定する)。timeout を allow に畳まなくなるので、`expect allow` のケースが時間切れで緑になることも無くなる
- 負荷の下でも通るよう timeout を延ばす・再試行する、は採らない (本番と同じ timeout を課すのがこのケースの目的。
  延ばすと「入力長で hook が timeout に殺される」退行を観測できなくなる。ケースの冒頭コメント参照)
- 再発して timeout と確定したら、hook 自体の 90KB で 1.4 秒 (budget の 14%) を削るか、このケースだけ負荷の少ない腕へ
  移すかを決める (直列の腕は並列の腕と同時に走るので、`SERIAL_TEST_DIRS` へ移しても負荷は減らない)

## 関連ファイル

- `tests/claude/test_deny_bare_tmux_kill.sh`
- `_claude/hooks/deny-bare-tmux-kill.sh`

## 進捗

- [x] 起票
- [x] timeout を allow と区別する (test(claude): deny-bare-tmux-kill の検査で timeout を allow に畳まない)
  - 確認: `HOOK_TIMEOUT=1` にした一時コピーで、90KB のケースが `期待=deny 実際=timeout (1 秒)` と案内の行を出す
    (変更前は同じ条件で `実際=allow`)。本来の 10 秒では全件緑
- [ ] 再発の観測 (waiting)
