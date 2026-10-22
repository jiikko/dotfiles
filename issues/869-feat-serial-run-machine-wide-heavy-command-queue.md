# 869 (feat): serial-run — 重いコマンドをマシン全体で 1 本ずつ流す

起票日: 2026-10-22

## 概要

このマシンでは Claude Code の session を複数起こし、それぞれの worktree で作業させている。`make test` / xcodebuild のような重いコマンドが
重なるとマシンの操作が重くなる。**重いコマンドをマシン全体で順番待ち (FIFO) にし、同時には 1 本だけ走らせる** CLI `serial-run` と、
Claude Code の session が打つ重いコマンドを自動でそれに包む hook を作る。

### 471 (pro-con 側の直列化) との関係

[471](epic/415/471-feat-pro-con-serialize-heavy-commands-across-sessions.md) は同じ問題を pro-con の中から解いている (テストの係のコマンドを
`lockman with <git-common-dir>/pro-con-locks/test` で包む。repo ごとの lock・待ちの上限 1 時間)。869 は別に作る:

- pro-con は今使っていない (ユーザー 2026-10-22)。素の Claude Code session どうしの重なりは 471 の仕組みでは止まらない
  (471 の「pro-con の外から同じ lock を取る形」は手で打つ 1 行で、自動では入らない)
- 目的が違う: 471 は repo ごと、869 は「マシンが重くならない」ことが目的なので**マシン全体で 1 本** (repo が違っても同時に走れば重い)
- lockman を使わない理由は下の「排他と順番」(SIGKILL された持ち主の lock が TTL の間残る。471 自身が「TTL の 30 分まで次の実行を止める」と書いている)
- 471 は閉じない (pro-con を再開するときの話として残す)。pro-con を再開するときは、テストの係も serial-run を取る形に寄せるかを 471 で決める

## なぜ要るか (過去の issue から)

- **258**: 2 session が同時に `make test` を回すと、golangci-lint のグローバルな lock で後発の lint が 3 本落ちた。直列で回し直すと rc=0
- **807**: `make test-go` の 22 プロジェクトはプロセス間で「今ビルド中」を共有しないので、冷えた cache では compile の 62% (1523 回中 942 回) が重複。
  2 session が重なればその分さらに増える
- **471**: 2 本の `make test` と 4 本の `mutate-verify` が重なり、14 コアでロードアベレージ 25。「負荷で落ちるテストが、PG の作業の合否を 1 回ぶん無駄にした」

効くこと: ①操作が重くならない ②**別の session の負荷**が重なって落ちる赤が減る ③session をまたいだ重複コンパイルが減る。

効かないこと: 「負荷の下でだけ落ちる」の記録 (27 ファイル) の多くは、**`make test` 自身の並列** (577「並列の負荷」・458「並列 97 本」・763「並列の負荷下」) が
負荷の元で、session をまたぐ直列化では消えない。テストの時間依存は個々の issue で直す (`avoid-wall-clock-assertions.md`)。
所要の before / after は未実測 (下の「計測」)。

## 決めること (ユーザー判断待ち)

1. **範囲**: 全 repo (Claude Code の hook が入口) / dotfiles だけ (Makefile が入口)。推奨は全 repo (`~/src` の repo の `make test` と xcodebuild も重さの元になる)
2. **lock の数**: 全部で 1 本 / 種類ごと (`xcode` と `default` など)。推奨は 1 本

以下はどちらも推奨の側で書く。

## 構成

| 置き場所 | 中身 |
|---|---|
| `src/serialrun/` | 本体 (Go の独立 module。`go.mod` / `main.go` / `queue.go` / `Makefile` / `README.md` / テスト) |
| `bin/serial-run` | 入口。`bin/runtimeout` と同じ数行の zsh で `go_autobuild_exec` を通す。**再ビルドは同期** (`--async` にしない。排他の判定を出す道具なので、旧版が「取れた」を返して走り出すのを避ける)。🚨 `go_autobuild_exec` はビルドに失敗すると rc=1 で抜ける (`bin/lib/go_autobuild.zsh`)。包んだ `make test` の失敗と見分けられないので、入口でビルドの失敗を 125 に写す |
| `_claude/hooks/serial-run-wrap.sh` | PreToolUse(Bash) hook。対象のコマンドを `serial-run -- …` に包む |
| `_claude/serial-run-targets` | 包む対象の一覧 (1 行 1 パターン) |
| `src/README.md` / `src/PACKAGES.md` | 一覧へ 1 行ずつ (`new-tool-requires-entrypoint-docs.md`) |

root の `make test` は `src/*/go.mod` を自動で見つけるので、本体のテストと lint は配線なしで乗る。

## 本体の内部構造

### 使い方

```
serial-run [-l <lock名>] [--max-wait <秒>] -- <コマンド> [引数…]   # 順番を待ってから実行し、子の rc を返す
serial-run status [-l <lock名>]                                     # 今走っているものと待ち行列を出す
```

### 状態の置き場

`~/.local/state/serial-run/<lock名>/` (既定の lock 名は `default`。テストは `SERIAL_RUN_STATE_DIR` で差し替える)。

```
<lock名>/
  seq          採番用のカウンタ。12 桁固定長の 10 進数
  run.lock     実行権の lock。持っているのは常に 1 プロセス
  history.log  1 本ごとに 1 行 (番号・待ち・所要・rc・cwd・コマンド)。追記だけ。計測と、順番が崩れたかの確認に使う
  queue/
    <seq 12 桁>.ticket   待っている / 走っている 1 本ごとの札。持ち主が開いている間 flock (LOCK_EX) を持ち続ける
```

- `$TMPDIR` (`/var/folders/…`) や `/private/tmp` に置かない (OS の掃除で消える)
- 札の中身は JSON: pid・開始時刻・cwd・コマンド行・repo 名 (cwd の `git rev-parse --show-toplevel`。取れなければ cwd)。`status` と待ちの表示が読む
- 🚨 **置き場は serial-run 自身が消さない・作り直さない**。走っている持ち主は今の `run.lock` の inode を flock しているので、置き場を作り直すと
  新しく来た人が新しい `run.lock` を取れて同時に 2 本走る。番号も 0 に戻って札の名前が使い回され、死んだ札の回収が生きている札を消しうる。
  初回に無ければ作るだけ。外から消された場合の排他は保証しない (README に書く)

### 排他と順番

`flock(2)` だけで決める。**生きているかの判定に pid を使わない** (pid は使い回される。flock はプロセスが死ねばカーネルが外すので、
SIGKILL・クラッシュ・マシンの再起動でも残骸の lock が残らない)。

1. **採番と札**: `seq` に flock を取り、**札を rename し終えるまで持ち続ける**。持ったまま:
   - 値を読んで +1 し、固定長で同じ位置に上書きする (truncate しない。途中で殺されても長さが変わらない)。🚨 tmp に書いて rename する形
     (`src/atomicfile` の流儀) にしない: flock は古い inode に残り、別のプロセスが新しい inode で番号を取って重複する
   - 札を `flockmark.Create` と同じ手順で出す (`.tmp-<id>` で作って flock し、中身を書いてから `<seq>.ticket` へ rename)。
     印の名前で作ってから flock すると、その間に他の待ち手から「持ち主のいない札」に見えて消される (`src/pro-con/flockmark/flockmark.go` の
     `Create` の注記。20000 回中 146 回再現している)。flockmark は pro-con の module の中なので import せず、同じ手順を書く
     (2 か所目。3 か所目が出たら共通 module に出す)
   - 採番と札の公開を 1 つの flock の中で行うので、「小さい番号を取ったのに札がまだ無い」間に大きい番号が先に入る形は起きない
   - `seq` が読めない (壊れている) ときは、`queue/` に残っている札の最大の番号 + 1 から作り直す (生きている札の番号とは重ならない)。
     札も無ければ 0 から
2. **待つ**: 0.5 秒ごとに `queue/` を列挙し、自分より小さい番号の札を順に `LOCK_EX|LOCK_NB` で開いてみる
   - 取れた = 持ち主が死んでいる → その札を消す (flock を持ったまま unlink するので、同時に見た他の待ち手と二重には消さない。
     番号は使い回されないので、同じ名前に別の生きた札が現れることはない)
   - 取れない = 生きている → 待ちを続ける
   - `.tmp-` で 1 分より古く、flock が取れるものは作りかけの残りとして消す (`flockmark.IsStaleTmp` と同じ扱い)
   - 自分より前に生きている札が無くなったら 3 へ
3. **実行権を取る**: `run.lock` に `LOCK_EX` (blocking)。札の順番は「公平さ」、run.lock は「排他」を持つ (札の列挙と判定の間に何かが
   割り込んでも、同時に 2 本走ることは run.lock が止める)
4. **実行**: 下の「子の起動」
5. **後始末**: 子が終わったら `history.log` に 1 行足し、札を消し、run.lock と札の fd を閉じる (閉じ忘れても、プロセスの終了でカーネルが外す)

### 待ちの上限と、長く持っている持ち主

- 待ちの既定の上限は 1 時間 (471 の `runLockGiveUp` と同じ)。`--max-wait` で変えられる。超えたら子を起こさず rc=125 で抜け、stderr に
  `serial-run: 待ちの上限 (60 分)` と持ち主 (pid・経過・cwd・コマンド) を出す。hang した 1 本がマシン全体の重いコマンドを無期限に止めるのを防ぐ
- 持つ側には上限を付けない (正当に長い `make test` を殺さない)。その代わり、待ち手の表示と `status` で、持ち主の経過が 30 分を超えたら
  `🚨 長く持っている` と pid・プロセスの状態 (`ps` の stat。Ctrl-Z で止まっていれば `T`) を出す。止めるかは人が決める

### 子の起動

- 子は `exec` で置き換えず、子プロセスとして起こす (終わってから札と lock を片付けるため)
- **lock と札の fd は子へ渡さない** (Go の `os.OpenFile` は O_CLOEXEC で開く。テストで子の fd に lock のファイルが無いことを固定する)。
  渡すと、子が残した背景プロセスが lock を握ったまま残り、全員が止まる
- 🚨 **子を別のプロセスグループに置かない** (呼び出し元のグループに残す)。別のグループに置くと、呼び出し元がグループへ SIGKILL を送ったとき
  (Claude Code のツールの中断がこの形かは未実測) serial-run だけが死んで lock が外れ、子は孤児として lock の外で走り続け、次の待ち手と 2 本同時に走る。
  同じグループなら一緒に死ぬ。端末の job control (Ctrl-Z・SIGTTOU) も子に効いたままになる (`src/runtimeout/main.go` の Setpgid の注記が、
  グループを分けたときに外れるものとして挙げている)
- serial-run が SIGINT / SIGTERM / SIGHUP を受けたら子へ転送し、猶予の後に `src/proctree` で子孫ごと止める
- **子が正常に終わった後は子孫を止めない**。残った孫は lock の外で走る (受け入れる。`status` には出ない)

### 入れ子

- 実行中の子には `SERIAL_RUN_HELD=<置き場の絶対パス>:<seq>` を環境変数で渡す。子の中から同じ置き場の serial-run が呼ばれ、その番号の札が
  今も run.lock の持ち主なら、待たずにそのまま実行する (待つとデッドロックする)
- 一致しなければ (親が終わった後に生き残った孫が呼んだ等) 普通に並ぶ
- serialrun 自身のテストは、包まれた `make test` の中で走るので外側の `SERIAL_RUN_HELD` を継承している。テストの中で外す (`env -u` / `t.Setenv`)

### rc

- 子の rc をそのまま返す。子がシグナルで死んだら 128+n
- serial-run 自身の失敗 (置き場が作れない・flock がエラーを返す・引数の誤り・待ちの上限・入口のビルドの失敗) は **125**。理由は stderr の 1 行で分ける
- 子のコマンドが見つからない 127 / 実行できない 126

### 待っている間の表示

stderr へ:

```
serial-run: 待ち 2 番目 (前: ~/src/obaket の make test 3 分 12 秒 / ~/dotfiles-wt-x の go test 待ち中)
```

順番が変わったときと 30 秒ごとに出す。実行に入るときに `serial-run: 実行 (待ち 4 分 05 秒)` を 1 行。Claude の session が「固まった」と
誤読しないための表示。ただし Bash ツールの上限で背景に回った後は、この表示はツールの出力ファイルにしか残らない。

## hook (入口)

- `_claude/settings.json` の PreToolUse(Bash) に `_claude/hooks/serial-run-wrap.sh` を足す。全 session に効く (pro-con の PG は user の hook を持ち込まないので除かれる。431)
- コマンド文字列が `_claude/serial-run-targets` のどれかに当たったら、コマンド全体を `serial-run -- zsh -c '<元のコマンド>'` に書き換えて通す
  - 🚨 **hook がコマンドを書き換えられるか (`hookSpecificOutput.updatedInput`) は未確認**。今の Claude Code の版で実測してから決める。
    書き換えられなければ deny で「`serial-run -- …` で包んで打ち直して」と返す形にする (打ち直しの往復が 1 回増える)
  - 元のコマンドの引用は shell の quote 規則でエスケープする (単引用符を含むコマンドで壊れないことをテストで固定)
  - 包むと、そのコマンドの中の `cd` / `export` は次の Bash 呼び出しへ持ち越されない。ツールのシェルの関数・alias・export していない変数も `zsh -c` の
    中では見えない。重いコマンドとそれらを同じ行に書く形は多くないので受け入れる
  - 既に `serial-run` を含むコマンドと、`SERIAL_RUN_HELD` が立っている環境では包まない
- 対象の初期値 (`_claude/serial-run-targets`):
  - `make test` / `make lint` / `make test-*` / `make -C <dir> test`
  - `go test` (どの形も。軽い 1 パッケージの go test まで並ぶのが重すぎたら外す)
  - `xcodebuild` / `swift build` / `swift test`
  - `mutate-verify` / `mutate-verify-list` (変異ごとに go test を回す。471 で重なりの元だった)
- 検出は字面で行う粗い判定。当てるのはコマンドの位置 (行頭・`&&` `||` `;` `|` の後) に来た語だけにし、引用符の中と `grep "make test"` のような
  引数の中は外す。それでも heredoc の本文などで外しきれない偽陽性は残り、そのときは関係のない読み取りのコマンドが最大で待ちの上限まで待たされる
  (偽陽性の数は `history.log` で後から数える)
- **字面で見えない重い処理は包まれない**: スクリプトの中から呼ばれる `go test` (`bin/mutate-verify` の中身・codex の fanout 等)。トップレベルの
  `mutate-verify` は上の一覧で包むが、それ以外は残る穴として受け入れ、効かない例が出たらその道具の側で serial-run を呼ぶ (入れ子は素通しになるので二重には取らない)
- 人が端末で直接打つコマンドは対象外。必要になったら alias / 関数を足す

## 失敗モード (設計で吸収するもの)

| 失敗 | 吸収の仕方 |
|---|---|
| 持ち主が SIGKILL・クラッシュ・再起動で死ぬ | flock はカーネルが外す。死んだ札は次の待ち手が消す |
| 呼び出し元のグループごと SIGKILL | 子を同じグループに置くので子も死ぬ (lock の外に子が残らない) |
| pid の使い回し | pid を生死の判定に使わない |
| 札の作成と flock の間の割り込み | `.tmp-` で作って flock してから rename |
| 採番と札の公開の間の追い越し | 札の rename まで `seq` の flock を持つ |
| `seq` の途中書き | 固定長の上書き (truncate しない)。読めなければ残っている札の最大 + 1 |
| 置き場の作り直し | serial-run は作り直さない (作り直すと排他が破れる) |
| 子が lock の fd を継承して背景に残る | close-on-exec。テストで固定 |
| 入れ子の呼び出しでデッドロック | `SERIAL_RUN_HELD` (置き場 + 番号) と run.lock の持ち主の一致で素通し |
| 待ち手が 2 人同時に同じ死んだ札を消す | 札に flock を取れた方だけが消す |
| hang・Ctrl-Z の持ち主が全員を止める | 待ちの上限 1 時間 (125)。長く持っている持ち主を表示で知らせる |
| 入口の自動ビルドの失敗が子の rc=1 に見える | 入口で 125 に写す |
| 時計が戻る | 順番は seq で決め、時刻は表示にしか使わない |

## テスト (本物の flock で)

`SERIAL_RUN_STATE_DIR` を `t.TempDir()` に向け、外側の `SERIAL_RUN_HELD` を外して、本物のプロセスを起こす:

- 排他: N 本を同時に起こし、子が「開始 / 終了」を 1 つのファイルへ追記する。区間が重ならないこと
- FIFO: 札の番号の順と実行の順が一致すること (起動の順ではなく採番の順で比べる)。採番と札の公開の間に差し込み口を置き、追い越しが起きないこと
- 死んだ持ち主: 実行中の serial-run を SIGKILL し、次の待ち手が入ること・札が消えること
- グループごとの SIGKILL: serial-run を含むグループを SIGKILL したとき、子も残らないこと
- 入れ子: 子の中から同じ lock の serial-run を呼んで止まらないこと / 一致しない `SERIAL_RUN_HELD` では並ぶこと
- fd を継承しない: 子の中から自分の fd に lock のファイルが無いこと
- `seq` が空・壊れている: 残っている札の最大 + 1 から続くこと
- 待ちの上限: 持ち主がいる間に `--max-wait 1` が 125 で抜け、子を起こさないこと
- シグナル: serial-run への SIGTERM が子に届き、子孫が残らないこと
- rc: 子の rc・128+n・125 / 126 / 127
- hook: 対象に当たる / 引数の中の `make test` には当たらない / 既に包まれている / 単引用符を含むコマンド (hook のテストは `tests/claude/`)
- 変異: 札の rename を「印の名前で作ってから flock」に戻す / 札の公開の前に `seq` の flock を離す / run.lock を取らない / close-on-exec を外す /
  子を別のグループに置く / 生死の判定を pid に替える / 待ちの上限を外す、がそれぞれ赤になること (`mutate-verify`)

待ちはテストでも時間で測らない (`avoid-wall-clock-assertions.md`)。区間の重なりと順番で判定する (待ちの上限のテストだけは、上限が仕様なので秒を使う)。

## 計測

- 未実測: 2 session が `make test` を同時に回す場合と、serial-run で直列にした場合の、それぞれの所要と「待ちを含めた所要」
- 未実測: 重なっている間の操作の重さ (load average と、エディタ・tmux の応答)。同じ 2 本の `make test` の組で比べる
- 未実測: Claude Code のツールの中断・上限での背景への移行が、serial-run と子へどのシグナルをどこへ送るか (段階 1 で測る。子のグループの設計の前提)
- trigger: 実装の後、最初に 2 session を並べた日に測り、この節へ書く (`history.log` が待ちと所要を残すので、測り直しは要らない)

## 対応方針 (段階)

1. 実測: hook の書き換え (`updatedInput`) ができるか / Claude Code のツールの中断・背景への移行で送られるシグナルと宛先
2. `src/serialrun` の本体とテスト (上の「テスト」まで) → 敵対的レビュー (自作の排他なので `adversarial-review-own-safeguards.md` が発動する)
3. `bin/serial-run` と一覧への登録
4. hook と対象の一覧 → 新しい session で効くことを headless で観測 (`.claude/rules/worktree-per-session.md` の手順)
5. 計測

## 関連ファイル

- `bin/runtimeout` / `src/runtimeout` — 入口と止め方の手本 (子のグループは手本にしない。上の「子の起動」)
- `src/pro-con/flockmark/flockmark.go` — 札の作り方の手本 (`Create` / `IsStaleTmp`)
- `src/proctree` — 子孫ごと止める
- `bin/lib/go_autobuild.zsh` — 入口の自動ビルド (失敗で rc=1)
- `src/lockman` — 使わない理由 (TTL 回収)
- 471 — pro-con 側の直列化 (repo ごと・lockman)。415 の要件 12 / 427 の段階 4
- 258 / 807 — 重なりで起きた問題の記録

## 反証レビュー (2026-10-22、Opus 1 体・読み取りのみ)

全数: P1 3 / P2 6 / P3 4 = 13 件。全部この本文に反映した。

- P1: 先行の 471 を引いていない (450 の引用は 471 の文だった) / 子を別グループに置くと、グループごとの SIGKILL で子が lock の外に残る / hang した持ち主が全員を無期限に止める
- P2: 260 / 616 / 137 は直列化で直らない (根拠から外した) / 「負荷で落ちる」の多くは make test 自身の並列 (効かないことに書いた) /
  採番と札の公開の間の追い越し / `seq` の途中書きで全員 125 (tmp + rename にすると番号が重複する) / 置き場の作り直しで排他が破れる /
  入口のビルドの失敗が rc=1 / `mutate-verify` が対象に無い
- P3: `zsh -c` で消えるのは cd / export だけでない・引用のエスケープ / 偽陽性の害は待ちの上限まで待つこと / テストが外側の `SERIAL_RUN_HELD` を継承 /
  `.tmp-` の回収と「履歴」の置き場が無かった
- 反証できなかったもの: flock を持ったままの unlink と新しい札の競合 (番号が使い回されない限り起きない) / 死んだ札の二重削除 / run.lock の排他そのもの
- 未確認のまま残るもの: Go の `os.OpenFile` の O_CLOEXEC は実装の知識で、実測していない (テストで固定する) / Claude Code の中断のシグナル (段階 1)

## 進捗

- [ ] 1. 実測 (hook の書き換え・中断のシグナル)
- [ ] 2. 本体とテスト・敵対的レビュー
- [ ] 3. 入口と一覧
- [ ] 4. hook
- [ ] 5. 計測
