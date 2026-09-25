# pro-con

PM (producer) と PG (consumer) を分けて Claude Code を並列に回すための TUI。
**設計の正本は issue 415** (epic `issues/epic/415/` の親 issue。残タスクは同じディレクトリの子 issue)。ここには実装側の事情だけを書く。

## 起動のしかた: 本物と模擬

```sh
bin/pro-con          # 本物: dispatcher が書くカードの記録を出す。依頼と回答は受付の箱へ (live。issue 424 / 427)
bin/pro-con --mock   # 模擬: claude は起動しない。模擬の backend (fake) が状態を進める (動作確認用)
bin/pro-con --view   # 見ているだけの画面: 動いている pro-con をそのまま映す。依頼・回答・attach を受けず (案内の行にも出さない)、dispatcher を起こさず、quit で閉じても何も止めない (画面の数にも入らない。epic 441)。書くのは画面の中継と、ctrl+r の引き継ぎ・落ちた画面の印の後始末だけ (issue 445)
bin/pro-con dispatcher   # 割り振り係: 受付の箱の適用・PM と PG の起動と再開・テストの係・見張り (画面を開くと、居なければ画面が起こす。旧名 daemon)。2 つ起動しない。🚨 PG を起動するので利用枠を使う
                         # 同時に動かす PG は --limit (既定 2) まで。利用枠 (`claude -p /usage` を 5 分ごとに読む) の 5 時間と週の大きい方が 80% 以上なら 1 本、95% 以上なら新しく起動・再開しない (ゲージの「PG n/m」と理由)
bin/pro-con config set limit 3 | set pm 1 | unset limit | show  # 止めずに PG の枠 (同時に動かす PG の上限) と PM の数を変える (issue 456)。受付の箱に置き、dispatcher が状態の置き場の settings.json に書いて次の Tick から使う (起動し直しても続く)
                         # 🚨 上限の優先: 利用枠の絞り (80% で 1 本 / 95% で 0 本) > 設定 (config set limit) > dispatcher の --limit > 既定 2。--limit は「設定が無いときの値」で、unset limit で戻る (画面が起こす dispatcher は --limit を付けない)
                         # PM の数は今は 1 だけを受ける (2 以上は 415 の論点 6 が決まるまで断る。今は 1 つで動くので dispatcher はまだ読まない。PM を起こさないのは --pm=off / config.toml の pm = "off")。settings.json が壊れていたら --limit で動き、理由をゲージに出す
bin/pro-con ps [--json]  # pro-con が起動したプロセスを役ごとに出す (dispatcher / PM / PG / テストの係 / 画面。pid・経過・状態・カード・コマンド)。読むだけ: 状態の置き場に書かず、dispatcher の lock も画面の印 (presence) も触らない。生きているかは ps を 1 回読んで決める (busy / idle は出さない)。pro-con の外の session は出さない
bin/pro-con dispatcher --stop  # dispatcher と、pro-con が起動した PG を止める。作業中のカードは次に dispatcher を起動したら続きから再開する (画面の終了も同じことをする)
                               # 人が止めた印 (`dispatcher-held`) を置く: 開いている画面は dispatcher を起こし直さず、ゲージに「止めてある」と出す。外すのは画面の c か、次に手で `pro-con dispatcher` を起動したとき (issue 459)
bin/pro-con card …   # PM / PG が使うカードの操作 (add / plan / ask / answer / handoff / review / rework / close / delete。plan --after は前のカードが完了するまで起動させない = issue 468。handoff は PM が PG の質問を人に回したことを履歴に残す。rework はレビュー待ちを直してほしい点つきで PG に戻す。delete は依頼の列ならすぐ消し、ほかは PG の session を止めてから消す = issue 451)。受付の箱に置くだけで、適用は dispatcher (issue 427)
bin/pro-con card guide  # PM の session に渡す指示書 (src/pro-con/pm-guide.md) を出す。dispatcher は依頼の列にカードが来たら・PG が質問したら PM を起動 / 再開してこれと新しいカード・質問を渡す (issue 437。PM は 1 つ・--limit に数えない)
bin/pro-con card guide --integrator  # 取り込みの係の session に渡す指示書 (src/pro-con/integrator-guide.md) を出す。dispatcher はカードがレビューの列に来たら取り込みの係を起動 / 再開してこれとカードを渡す。係はレビューし、master へ merge して push し閉じるか、差し戻す・人に回す (issue 487。1 つ・--limit に数えない)
bin/pro-con card attach C-001 shot.png --note "詳細の見た目"  # PG が作業の証拠 (画面の見た目・コマンドの出力) をカードに添付する (issue 453)。ファイルを受付の箱の files/ に写して依頼を置き、
                                         # dispatcher が状態の置き場の attachments/<カード>/ (0700 / 0600) へ移して記録に載せる。1 件 20 MiB・1 枚に 50 件まで。
                                         # 人間は詳細 (enter) の「添付」で見て o で画像を Preview に開く (.txt / .ans の文字は詳細の中にそのまま出す)。card show にパス。
                                         # カードが記録から外れたら (削除・書庫へ移した) dispatcher が添付を消す
bin/pro-con card move C-001 up   # レーンの中で 1 つ上 (down なら下) と入れ替える (画面の K / J と同じ。--repo でその repo の中の隣。issue 470)
bin/pro-con card list | show C-001 | wait C-001 --until review  # カードを画面なしで読む (--json も)。読むだけで、箱にも記録にも socket の wake / notify にも書かない (issue 442)。add は適用を待ってカード ID を返す
bin/pro-con card log C-001 [--follow] [--json]  # PG の活動 (応答の文と道具の呼び出し。例 `Bash: go test ./...` / `Edit: close.go`) を時刻の順に。再開で入れ替わった前の session から続けて出す。思考は Claude Code が中身を保存しないので出せず、道具の結果は長いので出さない。読むだけ (issue 467)
bin/pro-con log [--card C-001] [--follow] [--since 10m] [--json]  # dispatcher の出来事 (適用・除けた・起動・再開・止めた・削除・枠・watchdog・画面の数・画面を開いた / quit で閉じた) を読む。画面の出来事は画面が受付の箱に置き dispatcher が書く (--view の画面は置かない。issue 445)。記録は状態の置き場の events.jsonl (1 MiB で events.1.jsonl へ回す)。読むだけ (issue 444)
bin/pro-con --e2e <dir>  # e2e モード: 画面・dispatcher・受付の箱・記録は本物、PG と PM だけ台本どおりの偽物 (claude を起動しない。利用枠を使わない)
bin/pro-con e2e <start|keys|text|screen|wait|stop|scenario> <dir> ...  # Claude が e2e モードの画面を操作する口 (隔離した tmux サーバで動かす)
bin/pro-con card run C-001 -- make test  # PG がテストの係にコマンドを頼む。dispatcher が PG の worktree で 1 本ずつ順に実行し、結果を渡して PG を再開する (失敗は haiku が要約)。repo の lock (`<git の共通ディレクトリ>/pro-con-locks/test`) を `lockman with` で取って走らせ、外が持っていれば「pro-con の外が使用中」で待つ (issue 471)
                                         # -- の後ろは argv として 1 つずつ quote して記録する (引用はそのまま届く)。パイプや && を含む 1 行は `-- bash -c 'make test 2>&1 | tail'` で頼む (issue 463)
                                         # 🚨 PG に `pro-con card` を許すことは、その worktree で任意のコマンドを PG の permission の外で (dispatcher の権限で) 走らせるのを許すのと同じ。守っているのは「頼んだ場所がそのカードの PG の worktree」だけ
```

- どちらで動いているかはヘッダーに出る (`live: …` / `mock: …`。backend の `Describe`)。1 つの画面に両方のカードは混ぜない
- ライブアップグレードの状態ファイルは `$XDG_STATE_HOME/pro-con/live` と `…/mock` に分ける。新版には `--mock` を付け直す
- **本物 (live)**: カードは dispatcher が書く記録 (`$XDG_STATE_HOME/pro-con/live/cards.json`) から出る。書き込みは受付の箱 (`…/live/inbox/`) に
  置くだけで、記録へ適用するのは `pro-con dispatcher` (動いていないと箱に溜まり、ヘッダーに「適用待ち N 件」と出る)。
  操作はすべて受ける (新しい依頼 n / i・回答 r・削除 d・追加オーダー +・btw w・片付け x)。PG へ届ける・答えるのも dispatcher (issue 438):
  - **追記**は PG の turn の区切りで届ける (dispatcher は SendMessage を呼べないので、回答と同じ「止めて同じ session を再開」)。
    busy の間は積んで待ち、作業中のまま idle になったら再開する。`card ask` / `run` / `review` で turn を終えたら、その先の再開
    (回答・テストの結果・差し戻し) に添えて届ける。レビュー待ちに未達が残っていたら PG へ戻す。AskUserQuestion / 権限の確認で
    止まっている PG・落ちている PG には届けない (未達のままカードに見える)
  - **方針変更**は待たずに PG を止めて、指示を差し替えて同じ session を再開する (worktree の途中の変更は残る)。**別件**は元のカードの子の新しい依頼
  - **btw** は PG に届けない (止めない・文脈を汚さない。415 要件 9)。dispatcher が PG の出力の末尾とカードの記録から haiku で答え、
    答えはカードの履歴に出る (PG の出力が無ければ記録だけから答える)
  - **片付け**は画面が見ていた完了のカードだけを Archived にする。完了から 24 時間たったカードは dispatcher が自動で片付ける。
    片付けたカードは次の Tick で記録から書庫 (`…/live/cards-archive.jsonl`。足していくだけ) へ移す (issue 478: 記録を読む費用を
    作ったカードの総数に比例させない)。書庫は `card show` / `card list --all` / `card wait` が読む (画面と dispatcher は読まない)
- 作業中のカードには、**pro-con が起動した session だけ** (記録 `…/live/sessions.json` にあるもの) の様子 (PG の出力の末尾・pid) を足す。
  照合は記録の行の session id・短い id・pid が全部一致したときだけ (pid が違う = 外の shell で同じ session を再開したもの、は外れる)。
  Desktop や他の shell の session は出さず、選べない (選べると pro-con の外の session に入力・停止できてしまう)。
  attach は押した瞬間に一覧を取り直して照合し直してから撃つ。attach できるのは `claude --bg` の session だけ。
  attach から戻ると、その間に人間が打った文 (transcript の `origin.kind == "human"`) を原文のままカードの履歴へ残す
  (要約しない。受付の箱経由で dispatcher が書く。issue 428)。
  🚨 逆向き (外の shell から pro-con の session を attach / stop される) は Claude Code の側で止められない。検出は issue 427
- PG の出力は transcript (`~/.claude/projects/*/<sessionId>.jsonl`) の末尾 512KB から読み、`claude agents --json` とあわせて 3 秒ごとに裏で読み直す
- 詳細の「活動」(本物・`--view`) は、そのカードの session (起動の記録と `sessions-retired.json` の CardID で引く) の transcript を、読んだ位置の続きだけ 1 秒ごとに裏で読む (`live/activity.go`。画面が持つのは末尾 1000 件)。本文の末尾を見ていれば、足された活動を追って末尾に留まる
- **PG が今走らせているもの** (issue 473) は dispatcher が 10 秒ごとに集めて `…/live/doing.json` に書き、詳細・`card show`・ボードのバッジ
  (`▸ mut.sh 13分`) が読むだけで出す (画面は ps を回さない)。集めるのは、一覧と記録で pid が一致した PG の session の子孫のプロセス
  (Bash のコマンド・裏の shell)・transcript の末尾の裏のサブエージェントと結果待ちの道具の呼び出し。テストの係に頼んだコマンドの
  順番待ち / 実行中はカードの記録から足す。🚨 PG の外へ抜けたプロセス (`nohup … &` で親が launchd に移ったもの) は出ない
  (cwd で拾うと、人が worktree で開いた shell まで PG のものとして出す)。1 分集め直されていなければ詳細は古いと添え、ボードには出さない

## 画面と dispatcher のつながり (即時の割り振り・複数の画面・終了)

- **入力したら待たずに割り振る**: 受付の箱に置いた側 (画面・`pro-con card`) が、状態の置き場の Unix socket (`dispatcher.sock`。長い置き場は
  `/tmp/pro-con-<uid>/<hash>.sock`) で dispatcher を起こす (package `wake`)。dispatcher は適用したら画面へ知らせ、画面はすぐ読み直す
  (入力からカードが出るまで 約 4 秒 → 約 130 ms)。socket は速くするための口で、正しさは頼らない (届かなくても 3 秒のポーリングが拾う)。
  逃がし先 `/tmp/pro-con-<uid>/` の権限が緩ければ、直すのは dispatcher (listen する側) だけ。繋ぐ側 (画面・`card wait`・`log --follow`) は
  直さずにつながず、1 行知らせてポーリングで待つ (issue 445)
- **同じ置き場で画面を複数開いてよい**: 画面は開いている間 `screens/<id>.lock` を flock で持つ (package `presence`)。2 つ以上開いていれば
  ゲージに「画面 N」。Q → quit で閉じるとき、ほかに画面が開いていれば **この画面だけ閉じる** (dispatcher と PG は動いたまま)。最後の画面だけが止める
- **終了で pro-con が起動した claude を確実に止める**: 最後の画面が閉じるとき、dispatcher は pro-con の起動の記録 (`sessions.json` と、
  再開で入れ替わった前の session の `sessions-retired.json`) にある session を止め、`claude agents --json --all` で全部が stopped になったかを
  確かめて、残れば止め直す。止まらない session はカードと短い id で名指しする。pro-con が起動していない session には触らない
- 画面が起こした dispatcher (`--exit-without-screens 1m`) は、**画面が 1 つも無い状態が 1 分続いたら** PG を止めて抜ける (端末を閉じた・落ちた・
  kill -9 のように quit を通らずに消えても PG を残さない)。SIGTERM / SIGHUP でも、画面が消えていれば止めてから抜ける。
  手で起動した `pro-con dispatcher` (PM が CLI で使う形) は、`--stop` か最後の画面の quit まで動く
- 画面は開いている間、dispatcher が 10 秒以上回っていなければ起こし直す (落ちた・前の画面が止めている最中に開いた)。
  **人が `pro-con dispatcher --stop` で止めたときは起こさない** (状態の置き場の `dispatcher-held` が印。開いたときも起こさない)。
  画面が起こす dispatcher・画面の quit の停止には内部用の `--from-screen` が付き、印を置かない・外さない (最後の画面の quit で止めても、
  次に開いた画面は今までどおり起こす)。印を見てから起こすまでの間に `--stop` が来ても、起こされた dispatcher は印を見て回らずに抜ける。
  印がある間は、止めている途中で画面が開いても止めるのをやめない (issue 459)
- **クラッシュ・再起動の後は、起動したときに自動で復旧する** (コマンドも引数も無い。画面が起こしても手で起動しても同じ。issue 483)。
  dispatcher は一覧を取れた最初の Tick で 1 度だけ、記録にある作業中のカードの PG・PM・取り込みの係の session を調べる。
  **マシンの起動時刻 (`kern.boottime`) より前に始まり、`claude agents --json --all` に pid 無し・止まった state で残る**ものだけを
  待たずに再開する (作業中のカードは分解済みへ戻して同じ session を再開、テストの係の結果を待つカードは頼み直してから。落ちた回数に数え、上限なら人の番へ)。
  🚨 それ以外 (再起動より後に始まった session が居ない・--all にも居ない・起動時刻を読めない) は判定できないので復旧せず、出すだけにする
  (いつもの経路 = 自動の再開を 1 分待ってから戻す 458 に任せる。二重に起こさない)。結果は `pro-con log` (kind `recover`) に 1 行、
  ヘッダーのゲージに起動から 10 分「起動時: 復旧 N / 判定できない M」。判定の表は 483 の本文
- テストの係の実行の途中で dispatcher が止まった実行は、残ったプロセスを止めてから同じコマンドを 1 度だけ頼み直す
  (また中断したら rc=-1 で PG に返す。実行がマシンか dispatcher を落としている疑い)。結果を待つカードは、PG が一覧から消えても戻さない
- ヘッダーの「dispatcher が動いていない」は、lock のファイルの pid が居ないとき (Tick の古さの 2 分を待たずに出す。`--view` でも)

## 模擬 (ハリボテ) の中身

- 1 秒ごとに `Poll` し、fake の模擬時間が 1 分進む (カードが列を進む・PG が質問する・watchdog が停滞を拾う・リソースの順番が回る)
- attach (`a`) は本物の `claude attach` の代わりに `pro-con fake-attach <id>` を `tea.ExecProcess` で起動する。
  画面の明け渡しと戻りは本番と同じ経路を通る

| キー | 動作 |
|---|---|
| tab / shift+tab | repo タブの切り替え (global = 全 repo) |
| n | 新しい依頼 (PM へ)。repo のタブで出すとその repo がスコープになる |
| i | issue の一覧から選んで「これやって」と依頼する (repo のタブならその repo、global なら設定の全 repo。未完了だけ。epic は見出しの下に子)。Enter → 補足 (空でよい) → Enter |
| s | PG (consumer) の一覧を開閉 (詳細と同じく下から生える。担当カード・状態・実行中のコマンド・経過・実体の pid (本物のモードは backend が照合した pid。模擬は「模擬」)。pro-con の外の session は名前も本数も出さない。画面は `claude agents` を自分で読まない) |
| e | 選択中のカードの issue の md をエディタで開く ($VISUAL → $EDITOR → nvim。`tuikit/editor`) |
| x | 完了のレーンを片付ける (y/N 確認。repo のタブではその repo の分だけ。カードは消さず Archived にする。本物のモードでは dispatcher が書庫へ移す) |
| d | 選択中のカードを削除する (y/N 確認。依頼の列ならすぐ消える。ほかの列は「削除中」になり、dispatcher が PG の session を止めたのを確かめてから消える。1 分で止められなければカードを残して理由を履歴に書く。PG の worktree とブランチは消さない。消したことは dispatcher の記録に残る。issue 451) |
| o | 選択中のカードの画像の添付を Preview で開く (`open -a Preview`。複数の画像を 1 つの窓にまとめる)。文字の添付 (.txt / .ans) は詳細の中に出ている。それ以外のファイルは開かない (PG が用意したファイルを人間の権限で動かさない。パスは card show)。詳細を開いたままでも効く (issue 453) |
| y | 選択中のカードの issue の md のパス (素の値) をクリップボードへ |
| Y | 選択中のカードのタイトルと内容 (repo・状態・issue・依頼の原文・質問。整形した参照) をクリップボードへ。本文は `termsafe.PlainBlock` を通す |
| 1〜6 | そのレーンへ直接 (依頼 / 分解済み / 作業中 / 質問待ち / レビュー / 完了。数字はレーンの見出しの先頭に出る) |
| h / l / ← / → / ctrl+f | 左右のレーンへ (空のレーンにも止まる。フォーカスはレーンとカードの 2 段で、空のレーンではレーンだけに当たる) |
| j / k / ↑ / ↓ / ctrl+n / ctrl+p | 列の中で 1 枚 |
| ctrl+d / ctrl+u / space / f / pgdn / pgup | 半ページ |
| g / G / home / end | 列の先頭 / 末尾 |
| K / J | 選択中のカードを 1 つ上 / 下のカードと入れ替える (issue 470。レーンの中は上ほど優先度が高い)。選択はカードについていく。端では止めて知らせる (巻かない)。repo のタブではその repo のカードの中の隣と入れ替える。書くのは dispatcher (画面は受付の箱に置くだけ。速く続けて押しても、押した回数だけ動く)。分解済みの列は上から PG を起動する (回答・差し戻しを受けた再開は並びより先。バッジに ↻再開が先)。依頼の列は PM が上から分ける。`--after` の前のカードが終わっていなければ、上げても起動しない (履歴に理由)。詳細を開いている間の J / K は隣のカードへ送る |
| enter | 詳細の開閉。右から引き出しが滑り込み、カンバンの左端を残して重なる (glogx の issues の本文と同じ `tuikit/layout.ComposeDrawer`)。開いている間は j / k / ctrl+d / ctrl+u / g / G で本文をスクロール、J / K で同じレーンの隣のカードへ送る。本物のモードでは末尾の節が PG の「活動」(応答の文と道具の呼び出し。末尾を見ていれば新しいものを追う)、模擬では「出力」(出力の末尾)。カードへの操作 (a / r / + / ? / e / o / y / Y / d) は開いたまま効き、レーンの移動やタブは効かない。q / esc / h / ← / enter で閉じる |
| (入力中) | 入力欄・y/N 確認を出している間は、カンバンを暗い灰 1 色で描き、入力欄の行に地の色を敷く (キーは入力に取られ、カードは動かせない) |
| (選択の枠) | 選択中のカードは赤 (196) の太字の二重線 (╔═╗ ║ ╚═╝) の枠で囲む (issue 472。字と色は `ui/cursor.go` の定数)。選択が移ると、枠が元の位置から行き先まで 180ms で滑る (レーンを跨いでも。Excel のセルのカーソルの見え方)。滑る途中も枠は丸ごと見える (途中で消えて見えるちらつきを避けた)。カードの字の上では線で置き換えず、字を残して色だけ変える (横の辺は赤の上線 / 下線、縦の辺は赤の背景。上線は端末と tmux によっては出ない。issue 472)。カードの上下には 1 行ずつ空きがあり、枠はそこに描くので隣のカードを隠さない |
| (処理中の印) | 裏で処理中のカード (PG が turn の途中 = PG の status が busy / テストの係が実行中) は、バッジの頭に回る印 (⠋⠙⠹…) を出す。回すカードがある間だけ 100ms ごとに描き直す (`ui/spinner.go`) |
| (通知) | 操作の結果の通知は、ボードの右下の toast (`tuikit/toast`。glogx と同じ) に出る。右から滑り込み、数秒止まって、また右へ引っ込む。成功は ✓ 緑・失敗は ✗ 赤・断りや案内は … シアン (`ui/toast.go` の done / fail / info)。起動時の警告のような消すまで残す通知だけは下端の行 (esc で消す) |
| (案内の行) | カードへの操作と x は、選んでいるカードで効くときだけ明るく、効かないときは暗く出す (r は質問待ちだけ、a は session のあるカードだけ、e / y は issue の紐づいたカードだけ)。見ているだけの画面 (`--view`) では、押すと断る操作 (a / r / + / w / d / n / i / x / c) を出さない |
| ctrl+r | 新版へ切り替える (ライブアップグレード。「新版あり」のときだけ) |
| q / esc | 開いている板を 1 つ閉じる (PG の一覧 → 詳細)。何も開いていなくても終了しない (2026-09-25 に廃止) |
| Q | 終了の入力欄を開く。`quit` と打って enter したときだけ閉じる (本物のモードでは dispatcher と PG を止めてから閉じる。次に開くと続きから再開。ほかに画面が開いていれば、この画面だけ閉じる)。ctrl+c も同じ入力欄を開く (1 打では閉じない) |
| a | PG の session を開く (今は模擬) |
| r | 質問待ちのカードに回答する |
| + | 追加オーダー (tab で 追記 / 方針変更 / 別件)。**方針変更は y/N 確認** (y / enter だけが実行、他のキーは取り消し)。届いたかは詳細に「未達 / 届いた」で出る |
| w | btw (PG を止めずに状況を聞く。what's up)。本物のモードでは答えがカードの履歴に出る |
| ? | レーンの意味の表 (説明の正本は `card.State.Meaning`)。? / q / esc で閉じる |
| c | 人が止めた dispatcher を起こす (continue。y/N 確認。作業中のカードの PG が再開して利用枠を使う)。`pro-con dispatcher --stop` で止めた印があるときだけ効き、案内にもそのときだけ出る (ゲージは「dispatcher 止めてある (c で起こす)」)。`--view` では受けない (issue 459) |

入力欄 (n / r / + / ?) は readline の編集キーが効く (ctrl+h / ctrl+w / ctrl+u / ctrl+k / ctrl+a / ctrl+e / ctrl+b / ctrl+f …。
`tuikit/lineedit`)。入力中は最下行の案内が入力欄のキーに替わる。

**キーの語彙は [`docs/glogx-ui-guide.md`](../../docs/glogx-ui-guide.md) の 3 層 (vim / emacs 別名 / 動作) に従う**。
上下の移動は `tuikit/listnav.MotionOf` に渡す (glogx と同じ語彙を 1 か所で持つ)。画面固有の動作キーは先に捌く。

glogx と意味を変えている字 (`a` attach / `r` 回答 / `n` 新しい依頼 / `s` session の一覧) とその理由はガイドの §8。
`o` はガイドの「外で開く」の意味で、添付を `open` に渡す (issue 453)。`b` (半ページ上) はガイドの意味のために空けてあり、追加オーダーは `+`、btw は `w`、`?` はレーンの意味の表。

## 設定 (`~/.config/pro-con/config.toml`)

```toml
repo_roots = ["~/src"]      # この直下の git repo を列挙する (深くは掘らない。.git がファイルの worktree も拾う)
repos      = ["~/dotfiles"] # root の外にある repo を個別に足す
pm_repo    = "~/dotfiles"   # PM を起動する repo (PM はその下の worktree .claude/worktrees/pc-pm-<時刻> で動く。issue 437)
integrator = "on"            # 取り込みの係 (issue 487) を起こすか。"off" なら起こさない (レビューの列は人か外の Claude が扱う)。dispatcher の --integrator=on|off が勝つ
pm         = "on"           # "off" なら dispatcher は PM を起動も再開もしない (依頼の列のカードはそのまま置き、人か外の Claude が PM をする)
```

- **PM を起こさない口は 2 つ: dispatcher の `--pm=off` と設定の `pm = "off"`。`--pm` (on / off) を書けば設定より勝つ** (起動ごとに明示した方を優先する。
  設定で off にしていても `pro-con dispatcher --pm=on` で 1 回だけ起こせる)。off で起動した dispatcher は、そのことを出来事 (`pro-con log`) と dispatcher のログに 1 行出す。
  取り込みの係 (487) も同じ形で `--integrator=off` と設定の `integrator = "off"` を持つ。
  off でも、前の dispatcher が起こした PM は終了で止める。🚨 画面が起こす dispatcher には `--pm` を付けないので、画面から使うときは設定の方で決める
- ファイルが無ければ上の値が既定 (`$XDG_CONFIG_HOME` があればその下)。**壊れた TOML と知らないキーはエラーで起動しない**
  (書き間違えたキーを黙って無視すると「設定したのに効かない」が無音で起きる)
- 読めない root・repo でない `repos`・同じ名前の repo は警告にして起動する (通知行に件数と 1 件目)
- タブに出るのは「列挙した repo のうち、カードが 1 枚以上ある repo」だけ (~/src の下は数十 repo ある)。
  列挙に無い repo のカードは global タブにだけ出る。repo はディレクトリ名でカードと突き合わせる

## 新しい依頼のスコープ

`n` で出した依頼は、PM に渡す指示の先頭に前置きが付く (`backend.PMPrompt`。本文は書いたまま末尾に置く):

- **repo のタブ**から: 「この依頼のスコープは repo `<名前>` (`<パス>`) の中だけ。外のファイルを読んだり変更したりしない。issue とカードもこの repo に作る」
- **global のタブ**から: 「repo を指定していない。どの repo の作業かを判断し、複数にまたがるなら repo ごとにカードを分ける」

渡した指示の全文はカードの詳細に出る (何を渡したかを後から確かめられるように)。

## ライブアップグレード (ctrl+r)

使いながらソースを直すと、裏でビルドされて「新版あり ctrl+r」が出る。ctrl+r で自分自身を新しいバイナリへ入れ替える
(`syscall.Exec`。PID と端末はそのまま)。タブ・レーン・選択・開いている板・書きかけの入力と、模擬の状態を引き継ぐ。

- **ビルドするかの判定とビルドは `bin/lib/go_autobuild.zsh` (shim) に任せる** (`go_autobuild_spawn_if_stale`。glogx の
  autobuild.go と同じ形)。入力の指紋・`*_test.go` の除外・lock・失敗の記録と TTL は shim が正本で、Go 側に写さない
- 新版の判定は「起動したときのバイナリと、今パスにあるファイルが同じか」(同一性・更新時刻・大きさ)。ビルドの失敗は、
  最後に頼んだビルドより後の `.autobuild.failed` (ログは `src/pro-con/.autobuild.log`)。失敗の後も shim には尋ね続ける
- 勝手に切り替えない (入力中・確認中に入れ替わると、打っている途中の文字が迷子になる)。exec の前に、裏で外部コマンドを
  起こしている処理 (claude agents の取得・shim への問い合わせ) が終わるのを最大 5 秒待つ (glogx issue 211 と同じ理由)
- 引き継ぐ状態は `$XDG_STATE_HOME/pro-con` (既定 `~/.local/state/pro-con`) の `resume-*.json`。正常に終わったら消す。
  新版が異常終了したら残し、再起動の方法 (`PRO_CON_RESUME=<パス> bin/pro-con`) を出す。確認中 (y/N) は引き継がない。
  書きかけの入力は宛先 (カード / タブ) が変わっていたら戻さず、書いた文を消すまで残る行に出す (esc で消す)
- 使えるのは `bin/pro-con` から起動したとき (ソースのディレクトリの中のバイナリ) だけ。それ以外は ctrl+r で理由を出す
- 既知の制約: 旧版へ自動で戻る仕組みは持たない (新版が起動直後に落ちたら、直して起動し直す)。exec の直後にもう一度
  差し替わった新版は、次の差し替えまで「新版あり」にならない (起動時の Stat が、動いている版ではなくパスのファイルなので)

## テスト・lint の実行中

PG がテストや lint を実行している間も、カードは**作業中の列のまま** (列を移るのは担当が変わるときだけ)。バッジに
`▶ make test 3分` (リソースを占有していれば `▶ device: make e2e-device 3分`) を出す。watchdog の停滞の閾値は、
実行中は「見込みの所要の 2 倍」と通常の閾値の長い方 (`card.StallThreshold`。長いテストを停滞と誤判定しない)。

## issue の一覧から依頼する (`i`)

issue の読み方 (状態 = ファイルの位置、`epic/<name>/` の 2 段、`next/` の目印) は **glogx の issues viewer と同じ `glogx/issues`** に任せる
(`FindDirs` / `Scan` / `LoadMeta`。判定を 2 実装にしない)。並びも viewer と同じ。epic の親は「group 名と同じ番号の issue」
(glogx の `issues_view.go` の groupHead と同じ規則)。epic の見出しを選ぶと、未完了の子の一覧を付けた依頼になる。
依頼のカードは最初からその issue に紐づき、PM への指示に issue のパス (epic なら子の一覧) と repo のスコープが入る。

## issue のファイル

カードは issue を repo + 番号で持つ。`e` / `y` は、設定で列挙した repo の issue ディレクトリを `glogx/issues` で読んで番号の md を探す
(状態のディレクトリ・epic の下も探す。`next/` の claim の目印 = symlink は実体へ)。同じ番号が 2 つあれば
黙って選ばずエラー。カードに issue が複数あるときは最初の 1 つ (1 行目に `#415+1` のように残りの数を出す)。

## 構成

| package | 役割 |
|---|---|
| `config` | 設定ファイルと repo の列挙 |
| `agents` | `claude agents --json` で動いている session を一覧する (本物を読む。3 秒ごと・timeout 3 秒。取れなかったら 0 本にせずエラー) |
| `card` | ドメイン (カード・状態・不変条件の検査)。UI にも backend にも依存しない |
| `backend` | UI と状態の持ち主の境界 (`Backend` interface / `Command` / `Snapshot`)。UI はここより下を知らない |
| `fake` | 模擬 backend。本物に差し替えるときは `backend.Backend` を満たす実装を足し、`main.go` の 1 行を替える |
| `ui` | bubbletea v2 の TUI。状態は持たない (Snapshot を描き、Command を送るだけ) |

- `fake` の dispatcher / watchdog / リソース列は**模擬**で、本番の判定ではない。本番の判定を育てるなら fake から切り出す
- **見た目は B「枠」で合意** (2026-09-24。A 帯 / C カードと実物で見比べて選んだ)。列と詳細を角丸の罫線で囲み、選択中のカードがある列の枠だけを
  現在地色 (202) にする。状態の色は `theme/colors.yml` の意味に揃える (番号の手書きコピー。`ui/style.go` の冒頭)
- 表示の文言と配置にはテストを書いていない (テストはキー入力 → Command → 状態遷移のつなぎ込みだけ)
- 列幅の下限は 14 桁なので、**幅 89 桁未満の端末では行が端末幅を溢れる** (見た目を決めるときに列の畳み方と一緒に決める)
- **カードが列を移るときは滑らせる** (`ui/motion.go`。所要 800ms・終わり際に減速。演出の部品は `tuikit/anim` を replace で取り込む)。
  元の場所には点線の枠を残し、移動先の場所は着地まで空けて待つ。移動中のカードは地の色を変えず、枠も付けない (選択中のカードでも、着地するまで選択の枠を出さない)
- **カードは固有の地の色を持つ** (`ui/style.go` の `cardColor`。ID から決まるので列を移っても同じ色)。選択中は左右の両端に現在地色の ▌ ▐ を立て、タイトルを太字 + 下線にする (地は塗り替えない。3 案から a で合意)
- 🚨 **移動中も他のカードの位置と枠の高さは変えない**。列の中は「その列に入った順」に並べ (移ってきたカードは末尾に着地する)、
  枠の高さは画面の残りで固定する。`TestOtherCardsStayPutDuringMotion` が検査する

## e2e モード (Claude が画面を動かして確かめる)

画面・dispatcher・受付の箱・記録は本物のまま、PG と PM だけを台本どおりの偽物にする (`src/pro-con/dispatcher/e2e.go`)。claude は起動しないので利用枠を使わない。
置き場 `<dir>` の下に閉じる (`state/` = 状態、`repo/` = 偽の repo と PG の worktree)。本物の記録・tmux の件数・macOS の通知には触らない。

- 偽の PM: 依頼の列のカードを、その場で分解済みにする (e2e#1)
- 偽の PG の台本: 起動されたら質問する → 回答で再開されたらテストの係に `echo e2e-ok` を頼む → その結果で再開されたらレビューに出す

操作 (画面は置き場ごとの隔離した tmux サーバ `-L pro-con-e2e-<hash>` で動く。ユーザーの tmux の設定は読まない):

```sh
bin/pro-con e2e scenario ./tmp/e2e      # 依頼 → 質問 → 回答 → テストの係 → レビュー → Q → quit を通しで確かめる (約 30 秒)
bin/pro-con e2e start ./tmp/e2e         # 手で動かすとき: 起動
bin/pro-con e2e keys ./tmp/e2e n        # tmux のキー名で送る (Enter / Escape / Q / r / l ...)
bin/pro-con e2e text ./tmp/e2e "依頼の文"
bin/pro-con e2e wait ./tmp/e2e "質問待ち (1)" 30
bin/pro-con e2e screen ./tmp/e2e        # 画面の文字
bin/pro-con e2e stop ./tmp/e2e          # Q → quit で閉じる (dispatcher と偽の PG も止まる)
```

`tests/pro-con/test_e2e_scenario.sh` が make test の中で `scenario` を回す (tmux が無ければ skip)。

