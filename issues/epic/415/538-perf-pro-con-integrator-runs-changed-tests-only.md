# 538 (perf): 取り込みの係のテストを、差分に関係する分だけにする (`make test` → `make test-changed`)

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-091) に、ユーザーが「差分に関係する分だけ」と答えた。

- 今の取り込みの係は、カード 1 枚ごとに repo 全体の `make test` と `make lint` を回す (`src/pro-con/integrator-guide.md` の役目 2)。
  2026-09-27 00:55 以降の実測で 1 枚約 6 分 (道具の時間 744 秒のうち 2 回で 709 秒)。1 枚ずつ順に取り込むので 4 枚で約 25 分
- `make test-changed PATHS="..."` (`scripts/test_changed.sh`) は、変えたパスから回すべき既存のテストのターゲットを導いて回す (共通の module の利用者も)。
  どの写像にも当たらないパスは fail にする (黙って skip しない)

## 期待する動作

- 取り込みの係の役目 2 を、`make test` と `make lint` の代わりに、merge した差分のパス (`git diff --name-only origin/master...HEAD`) を渡した
  `make test-changed PATHS="..."` にする。pro-con の `src/pro-con` の `make test` / `make lint` の段も、test-changed の src の腕が lint と test を回すなら重ねない (確かめて決める)
- 写像に当たらないパスで fail したときは、黙って飛ばさず root の `make test` に倒す (test_changed.sh の方針どおり)
- `--after` の付いたカード (468) の「合わせた結果を同じ判断に当たる所のテストで確かめる」は、差分のパスに先のカードの変更も入るように取る (取り込み用の worktree は origin/master から作るので、先のカードが入っていれば差分の外になる。必要なら先のカードのパスも足す)
- 🚨 性能の主張は測って書き戻す: 変えた後の 1 枚あたりの時間を、上の実測と同じ数え方で測って本文に残す (perf-claims-need-measurement)

## 関連ファイル

- `src/pro-con/integrator-guide.md` の役目 2 / `scripts/test_changed.sh` / `Makefile` の `test-changed`

## 関連

- 487 (取り込みの係) / 471 (重い処理の直列化) / 468 (`--after`)

## 経過

### 2026-09-27 C-091 (PG): 指示書の役目 2 を書き換えた

- `src/pro-con/integrator-guide.md` の役目 2 を「差分のパスを渡した `make test-changed`、写像に無いパスで止まったら repo の `make test`」にした
- pro-con の段は重ねない: test_changed.sh の `src/*/*` の腕が `make -C src/<proj> lint` と `test` を別々に回す (`scripts/test_changed.sh` の末尾) ので、`src/pro-con` の `make test` / `make lint` は差分に src/pro-con が入れば既に回る
- 指示書は dotfiles 以外の repo にも使う。dotfiles の root には `make lint` のターゲットが無く、`make test-changed` も dotfiles だけにあるので、「`make test-changed` の無い repo では従来どおり」を残した
- `--after` の先のカードのパスは、master の merge commit から取る (`git log --merges -1 --format=%H --grep="'<ブランチ>'" origin/master` → `git diff --name-only <merge>^1 <merge>`)。
  取り込みの係の merge の件名は `Merge branch 'worktree-pc-c-086' into HEAD` の形で、C-086 の merge (33dd6906) で 9 パスが取れることを確かめた。`pro-con card show` の差分は作業中のカードにしか出ないので使えない
- 🚨 未了: 変えた後の 1 枚あたりの時間はまだ測っていない (取り込みの係が新しい指示書で動いてから、上の実測と同じ数え方で測って書き足す)
