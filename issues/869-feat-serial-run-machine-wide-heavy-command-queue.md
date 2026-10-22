# 869 (feat): serial-run — 重いコマンドをマシン全体で 1 本ずつ流す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

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
2. **待つ (イベント駆動。ポーリングしない)**: `queue/` を列挙し、**自分の 1 つ前の札** (自分より小さい番号のうち最大のもの) に
   blocking の `flock(LOCK_EX)` をかけて眠る。起こすのはカーネルで、起きるのは前の札の持ち主が終わるか死んで lock を外したときだけ
   - 起きたら、その札がまだ在るかを見る。在る = 持ち主が死んだ (終わった持ち主は札を消してから lock を外す) → flock を持ったまま消す。
     無い = 終わった → 何もしない。どちらも、掴んだ fd を閉じてから列挙し直し、新しい「1 つ前の札」でまた眠る
   - 前に札が無ければ 3 へ
   - 新しく来た人は必ず自分より大きい番号になる (採番と札の公開を 1 つの flock の中で行うため) ので、自分より前の札は減る一方で、
     眠っている間に「1 つ前」が入れ替わることはない。だから 1 つ前だけを見れば足りる
   - 1 枚の札で眠るのは、その 1 つ後ろの待ち手だけ (行列の鎖)。待ち手が N 本いても、順番が 1 つ進むときに起きるのは 1 本だけ
   - `.tmp-` で 1 分より古く、flock が取れるものは作りかけの残りとして消す (`flockmark.IsStaleTmp` と同じ扱い)。周期では見ず、起きて列挙するたびに見る
   - blocking の flock は止められないので、専用の goroutine で待つ。本体は「flock が返った」「待ちの上限のタイマー」「30 秒の表示のタイマー」
     「シグナル」を `select` で待つ。上限・シグナルで抜けるときは自分の札を消してからプロセスごと終わる (眠っている goroutine の lock は、
     プロセスの終了でカーネルが外す)
   - 待機中に定期的に起きるのは、30 秒ごとの表示と、2 分ごとの保険の見直しだけ (保険はユーザー指示 2026-10-22: blocking の待ちが何かで
     起こされなかったときに列挙し直す)
   - 1 つ前の札が flock で待てないときは blocking で眠らず、5 秒ごとに見直す: ①持ち主は死んだが子が走っている (下の「子の起動」の孤児) ②札を開けない
     (権限・fd の枯渇。生死を決められないので待ち続け、表示に出す)。🚨 開けない札で blocking の待ちを即座に起こし返すと空回り (CPU 100%) になる
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
- 🚨 **serial-run だけが pid 宛ての SIGKILL で死ぬと、子は走り続ける** (札の flock は持ち主の死で外れる)。子の pid と開始時刻 (ps の lstart) を札に書き、
  持ち主が死んでいても子が同じ開始時刻で生きていれば、その札を生きている扱い (孤児) にする。子が終わるまで後ろは入らない

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

順番が変わったとき (= 上の「待つ」で起きたとき) と 30 秒ごとに出す。実行に入るときに `serial-run: 実行 (待ち 4 分 05 秒)` を 1 行。Claude の session が「固まった」と
誤読しないための表示。ただし Bash ツールの上限で背景に回った後は、この表示はツールの出力ファイルにしか残らない。

## hook (入口)

実装した形 (設計の段階の「`zsh -c` で丸ごと包む」から変えた。理由は下の実測):

- `_claude/settings.json` の PreToolUse(Bash) に `_claude/hooks/serial-run-wrap.sh` を足した。全 session に効く (pro-con の PG は user の hook を持ち込まないので除かれる。431)
- 本体は `serial-run hook` (`src/serialrun/hook.go`)。**重い単純コマンドの直前にだけ `serial-run -- ` を差し込む** (`cd a && make test && git push` →
  `cd a && serial-run -- make test && git push`)。`updatedInput` で返し、`permissionDecision` は出さない
- 🚨 **実測 (Claude Code 2.1.308、2026-10-22、`claude -p --settings <使い捨ての設定>` の headless で)**:
  - `updatedInput` の書き換えは効く (モデルが頼んだ `echo ORIGINAL` の代わりに書き換えた方が走った)。`permissionDecision: allow` を付けなくても効く
  - **許可ルールは書き換えた後のコマンドで判定される** (元のコマンドへの deny は素通りし、書き換えた後への deny は効いた)。
    丸ごと `zsh -c` で包むと `make test && git push --force` の `git push` への deny が効かなくなるので、差し込む形にした。差し込む形なら `cd` / `export` も次の呼び出しへ持ち越される
- 差し込まない形 (読み切れない・壊しうるものは元のまま通す): 引用符・コマンド置換の中 / heredoc・`case`・`((` を含む / 閉じていない引用 / 語の直後の `(` (`arr=(…)`・関数定義) /
  `PATH=` を含む (前置きの PATH や `export PATH` の後では `serial-run` が見つからず rc=127 になる) / 既に `serial-run` を含む
- 重さの判定 (`isHeavy`): `make` の `test` / `lint` / `test-*` / `lint-*` の target (`-C` `-f` の値は飛ばす。`-j` は次が数値のときだけ値) / `go test` /
  `swift build|test` / `xcodebuild` (`-version` `-list` `-showBuildSettings` などの情報表示は除く) / `mutate-verify(-list)`。
  前に付いた `env` / `nice` / `nohup` / `command` / `noglob` / `runtimeout <秒>` は外して判定し、差し込むのはその前 (`serial-run -- runtimeout 600 make test`。
  中に入れると待ちが runtimeout の上限を食う)
- hook の入口 (`serial-run-wrap.sh`) は fail-open: どこで失敗しても元のコマンドのまま通す。PATH 上の `serial-run` がこの checkout のものに解決しなければ差し込まない。
  止め方は Claude Code の環境に `SERIAL_RUN_HOOK_DISABLE=1`
- **字面で見えない重い処理は包まれない**: スクリプトの中から呼ばれる `go test` (`bin/mutate-verify` の中身・codex の fanout 等)。効かない例が出たらその道具の側で serial-run を呼ぶ (入れ子は素通しになる)
- 人が端末で直接打つコマンドは対象外。必要になったら alias / 関数を足す
- **待つことを LLM に先に知らせる** (ユーザー要望 2026-10-22): Bash ツールはコマンドが終わるまで出力を返さないので、待っている間の LLM には
  「長く走っている」としか見えない。差し込むときに既定の lock の待ち行列を読み、前に誰かいれば `additionalContext` に「順番待ちに入る・前に N 本・
  先頭の持ち主・出力が返らないのは固まったのではない・run_in_background / `serial-run status`・上限 1 時間で rc=125」を添える
  (前に誰もいなければ何も添えない)。🚨 実測 (2.1.308): PreToolUse の `additionalContext` は `updatedInput` と一緒に LLM へ届く
- 待ちの最初の 1 行の末尾に `(状況は serial-run status)` を添える (背景に回った後の LLM が、別の呼び出しで様子を見られる)

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
| 待ち手が増えるほど起床が増える | 1 つ前の札で眠る鎖にする。順番が 1 つ進んで起きるのは 1 本だけ |
| 眠っている flock を上限・シグナルで止められない | flock は専用の goroutine で待ち、本体は select で待つ。抜けるときはプロセスごと終わる |

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
  子を別のグループに置く / 生死の判定を pid に替える / 待ちの上限を外す / 待ちを「短い sleep で列挙し直す」ポーリングに戻す、
  がそれぞれ赤になること (`mutate-verify`)
- イベント駆動: 待ちのループに起きた回数を数える差し込み口を置き、前の札の持ち主が動かない間は回数が増えないこと
  (時間ではなく回数で見る。ポーリングに戻す変異で回数が増えて赤になる)。前の 2 本が順に終わったときは 2 回だけ起きること
- 鎖の途中の待ち手が SIGKILL で死んだとき、その後ろの待ち手が起きて札を消し、さらに前の札で眠り直すこと

待ちはテストでも時間で測らない (`avoid-wall-clock-assertions.md`)。区間の重なりと順番で判定する (待ちの上限のテストだけは、上限が仕様なので秒を使う)。

## 計測

- 未実測: 2 session が `make test` を同時に回す場合と、serial-run で直列にした場合の、それぞれの所要と「待ちを含めた所要」
- 未実測: 重なっている間の操作の重さ (load average と、エディタ・tmux の応答)。同じ 2 本の `make test` の組で比べる
- 未実測: Claude Code のツールの中断・上限での背景への移行が、serial-run と子へどのシグナルをどこへ送るか (段階 1 で測る。子のグループの設計の前提)
- 未実測: 待機中の負荷 (待ち手 1 本の CPU 時間と RSS。待っている 10 分の間に増えた CPU 時間を `ps -o time` で読む)
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

## 決めたこと

- 2026-10-22 (ユーザー): 待ちはポーリングにせず、イベント駆動にする (「待つ」の段)。この段は反証レビューの後に書き換えたので、まだ反証レビューに通していない (段階 2 の敵対的レビューで見る)

## 実装 (2026-10-22)

- `src/serialrun` (main.go / queue.go / hook.go とテスト)、`bin/serial-run`、`_claude/hooks/serial-run-wrap.sh`、`tests/claude/test_serial_run_wrap.sh`、
  `.github/workflows/src_serialrun.yml`、`src/README.md` / `src/PACKAGES.md` / `Makefile` の zsh の構文検査の一覧
- テスト: `go test -race` 22 本を 3 回連続で緑。hook の入口 7 件。`make lint` 0 件 (v2.15.0)
- 変異 (`mutate-verify-list`。全部 red を確かめた): 採番と札の公開の間で seq の flock を離す / run.lock の fd を子へ渡す / 子を別のグループに置く /
  待ちの上限を外す / blocking の待ちを 100ms の sleep に置き換える / 保険の見直しを外す / 入れ子で待つ / 死んだ札を回収しない / seq が札を見ない /
  SIGTERM で子を止めない / 孤児の子を見ない / run.lock を取らない / QUIT で死に直す / hook: heredoc の打ち切りを外す / 代入を飛ばさない / make の値の
  オプションを飛ばさない / 許可の判断 (allow) を出す / 包みのコマンドを外さない / PATH= の打ち切りを外す / `&>` を区切りと読む / 語の直後の `(` を許す。
  空回りの防御 (blocking を即座に起こさない + 判定不能の札は短い見直し) は 2 ファイルにまたがるので、写しに両方を戻して red (1 秒に 9168 回起きた) を確かめた
- 緑のまま通った変異 1 本: hook の、置換の結果の仮の値を変える形 (`no-subst-skip`)。仮の値が効くのはコマンド置換がコマンド語そのものになる形
  (`$(echo make) test`) だけで、そこで差し込んでも害は無い。置換の中への差し込みは、`(` で字句を打ち切る作りが別に止めている。等価に近い変異として受け入れる

### 敵対的レビュー (2026-10-22、Opus 1 体。実装の HEAD に対して)

全数: P1 1 / P2 3 / P3 5 = 9 件。対応 7 / 記録 2。

- 対応した (どれも上の変異で守った):
  - P1 開けない札 (chmod 000・sudo で作られた札・EMFILE) で待ちが空回りし CPU 100% (レビュアーが再現: 3 秒で 23,922 回)
  - P2 serial-run だけを pid 宛てに SIGKILL すると子が残り 2 本走る (再現) → 札に子の pid と開始時刻を書き、孤児の子を待つ
  - P2 `PATH=… make test` を書き換えると rc=127 (再現) → `PATH=` を含むコマンドは書き換えない
  - P3 run.lock を外しても全テストが緑 → 持ち主の札を外から消しても 2 本目が入らないテストを足した
  - P3 `&>` を区切りと読む・`arr=(go test)` をサブシェルと読む → 直した
  - P3 グループへの SIGQUIT で rc=2 と goroutine のダンプ → QUIT では死に直さず 128+n を返す
  - P3 `xcodebuild -version` 等も待たされる・`env` / `nice` / `runtimeout` を前に付けた形と `make -j test` が包まれない → 直した
- 記録だけ (決めるのはユーザー):
  - P2 **既存の許可ルールが外れる**: 他の repo の settings に `Bash(make test:*)` 2 件・`Bash(go test:*)` 4 件・`Bash(make lint:*)` 2 件がある
    (例: `~/src/pj_energy_matching/.claude/settings.local.json`)。書き換えた後のコマンドはこれらに当たらないので、auto でない許可の形では毎回確認が出る
    (deny / ask なら素通りする)。必要なら各 repo に `Bash(serial-run -- make test:*)` の形を足す
  - P3 **待ちの上限 1 時間は前に並ぶ全員の合計で尽きる**。20 分の make test が 3 本並ぶと 4 本目は rc=125 (stderr に `待ちの上限` と持ち主が出る)
- 未確認のまま: Claude Code のツールの中断・TaskStop がどのシグナルをどこへ送るか (孤児の子の扱いで、pid 宛ての KILL でも順番は守られる形にした)

## Makefile への組み込み (残り。2026-10-22 にユーザーと合意)

hook は Claude Code の Bash ツールが打ったコマンドにしか効かず、字句で見えるものしか包めない。dotfiles の `make test` などの入口の target が
自分で serial-run を取れば、人の端末・pre-push・スクリプト・サブエージェント・codex のどこから呼ばれても並び、字面は `make test` のままなので
他の repo の許可ルール (`Bash(make test:*)`。敵対的レビューで記録だけにした P2) も当たる。

- 形: 入口の target だけ、`SERIAL_RUN_HELD` が無ければ `exec serial-run -- $(MAKE) $(MAKECMDGOALS)` で自分を起こし直す。入れ子 (中の `make -C src/x test`・
  hook で包まれた `make test`・テストの中の `make`) は `SERIAL_RUN_HELD` の素通しで二重に取らない
- CI (`CI=true`) では包まない (runner は 1 台に 1 本)
- 🚨 呼ぶのは worktree の版ではなく `~/dotfiles/bin/serial-run` (本体 checkout) に固定する。状態の置き場は 1 つなので、札・seq の形式が版で違うと食い違う。
  worktree での初回のビルド待ちも避けられる
- 粒度: 最初は `make test` 全体で 1 本 (go test のレーンごとに取る形は変更が大きい)
- 所要を測る bench・テストは待ちも含めて測る。待ちは `history.log` に別に残るので、測る側で差し引ける
- hook は、Makefile が包む入口 (dotfiles の `make test`) を見つけたら差し込まないようにする (二重の表示を避ける)
- ほかの repo は共有の断片 (`include ~/dotfiles/make/serial-run.mk` のような 1 行) で同じことができる。Makefile の無い repo の xcodebuild・素の go test は hook が拾う

## 進捗

- [x] 1. 実測: hook の書き換えは効く・許可ルールは書き換えた後で判定される (上の「hook」)。中断のシグナルは未実測
- [x] 2. 本体とテスト・敵対的レビュー (上の「実装」)
- [x] 3. 入口と一覧
- [x] 4. hook (push して `~/dotfiles` に pull した後の新しい session から効く。効いたかの観測は下の残り)
- [ ] 5. 計測 (trigger: 2 session を並べた最初の日。`~/.local/state/serial-run/default/history.log` の待ち・所要を読む)
- [x] 待つことを LLM に先に知らせる (`additionalContext`) / 待ちの行に `serial-run status` の案内。変異 2 本 (知らせを外す / 案内を毎行に出す) が red
- [ ] **dotfiles の Makefile に組み込む** (ユーザーと合意 2026-10-22。下の「Makefile への組み込み」)
- [ ] 新しい session で hook が効くことの観測 (headless の session で `make -n test` 相当を打たせ、書き換えを見る)
