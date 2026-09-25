# 450 (retro): 並行セッションの依頼 7 件 (flaky 2 本・CI・rule の条件ロード・e2e・lint・viewer 2 本) の振り返り

起票日: 2026-09-25

## 概要

dotfiles-5c から順に受けた依頼を dotfiles-7d が処理した: tmux-toast の flaky (6849eed4) / Tests の赤 (95936d0d) / 414 の rule の paths: 化 (eb0bc7a7、一部を ef3f6191 で戻した) /
pro-con の e2e 回帰テスト (a6fb64a0) / pro-con の lint の強化 (6965342c) / 442 card list・show・wait (ebd25beb) / codex_fanout の flaky (f866daf4) / 443 pro-con screen (c244ece4)。

## どこで踏んだか

1. **push の rc をパイプ越しに見て、未 push の commit を持つ worktree を消した** (codex_fanout の修正)。`git push … | tail -1 && … && git worktree remove` で
   push が non-fast-forward で弾かれたのに後段が走った。`verify-execution-not-just-exit-code.md` と `worktree-per-session.md` に同じ形が書いてあるのに踏んだ。
   git の object から拾い直して復旧した
2. **「完了」の観測値が、壊れた経路からも立つ**: e2e で「最後の画面を閉じたら stop-result=ok」を見ていたが、最後の画面が止めなくても 1 分後に別の経路
   (画面が無い状態が続いた dispatcher) が ok を書くので、変異「止めない」が緑のまま通った。dispatcher.log の出どころの行で経路を見分けて直した
3. **rule の条件ロード (paths:) で、dotfiles の CLAUDE.md が名指しで禁じている形 (行動で発火するテスト作法) を入れた** (414)。5c の指摘で戻した。
   触る前に読むべき規約を、依頼文にあった注意 (Read でしか発火しない) だけで判断した
4. **bin/mutate-verify の --expect が `-` で始まるパターン (`--- FAIL:`) を grep のオプションとして読み、照合が空振りする**。狙ったテストが落ちているのに
   「別の検査が先に落ちている」と出た (`grep -qE "$expect"` に `-e` が無い)
5. **テストの後始末が、検出した後に固まる**: 「Put が待たない」を見るテストで、変異 (Put が待つ) を当てると検出はするが、defer の Close が詰まった書き出しを
   待って go test の上限まで固まった。失敗の出口でも詰まりを解く形に直した

## 次に効きそうな改善

- 1: **既存ルールで足りている** (新しい規範は要らない)。守れなかったのは規範の不在ではなく、長いワンライナーに push と破壊的な後片付けを同居させたこと。
  → 却下: 既存の `verify-execution-not-just-exit-code.md` の「成否で後段を走らせる `&&` のつなぎ」が正本。ただし hook で止められるなら止めたい (下の issue 候補)
- 1 の機械化: `git push … | …` のパイプの後に `&& git worktree remove` が続く Bash を PreToolUse で止める hook は作れる (dotfiles の hook の置き場)。
  → 切り出し先: 新規 issue 候補 (ユーザーの判断待ち)
- 2: **完了・成功を表す観測値を検査に使うときは、その値を立てる経路を全部数え、検査したい経路以外からも立つなら経路を見分ける観測を併置する**。
  `mutation-verify-new-tests.md` の「逆向きの問い: その観測値が進んだのに、目的が達成されていない状態を作れるか (十分性)」と同じ形
  → 却下: 既存の十分性の項で足りる (今回はその問いを変異で後から当てて見つけた = 規範どおりに機能した)
- 4: bin/mutate-verify の grep に `-e` を付ける (`--expect` / `--baseline-expect` の照合 4 か所)。道具の局所的な直し
  → 切り出し先: 新規 issue 候補 (直すかはユーザーの判断待ち。直さないなら bin/mutate-verify のヘッダの「--expect は grep -E」の注記に「`-` で始めない」を足す)
- 3・5: 局所的 (その場で直した)。提案しない
