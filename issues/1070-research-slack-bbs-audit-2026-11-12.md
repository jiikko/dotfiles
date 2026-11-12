# 1070 (research): src/slack-bbs の全観点の監査 (2026-11-12。直接実行)

起票日: 2026-11-12

## 概要

`/all-audit-on-diy slack-bbs` の 2 回目。対象は src/slack-bbs (`git ls-files src/slack-bbs` で 52 ファイル・14,597 行、`e2fec4619` 時点)。
前回 (1065、2026-11-11) からの差分 22 commit と、前回読まなかった範囲を中心に見る。本文は観点ごとに足していく。

## 走査した母集合

- 対象: `git ls-files src/slack-bbs` で 52 ファイル・14,597 行。前回 (`732df8baa`) からの差分は 22 commit・28 ファイル・+2,856 / −171 行
- 対象を名指しする外のファイル (`git grep -l slack-bbs -- ':/'`): `.github/workflows/src_slack-bbs.yml`・`_claude/skills/slack-bbs/SKILL.md`・`docs/glogx-ui-guide.md`・
  `src/tuikit/termsync` (1069 で移した)・issue 706 / 1066 / 1069 ほか
- 機械の検査 (`c455f5c5b`): `go vet` 0・`make lint` 0 issues・`go mod tidy -diff` 差分 0・`deadcode` 2 件 (下の dead-code)・`deadcode -test` 0・
  `dupscan sweep src/slack-bbs` (単位 531) と `dupscan sweep -focus slack-bbs src` (単位 10,049)

## 観点ごとの結果

| 観点 | 結果 | 攻めた所・報告しなかった理由 |
|---|---|---|
| duplication | 1 (直した。ユーザーの依頼「src の中で共通化する余地があれば切り出す」を兼ねる) | 取り消しで抜けられる待ちを共有 module `ctxwait` に寄せた (commit「refactor(src): 取り消しで抜けられる待ち (ctx 付きの sleep と、取れるまで待つ flock) を共有 module ctxwait に寄せる」)。ctx 付きの sleep は slack-bbs・doctor・process_supervisor の 3 か所、取れるまで待つ flock は slack-bbs・ratelimit・zundamon-kaisetsu の 3 か所。1065 で「3 か所目が出たら再考」とした件。寄せなかったもの: pro-con の待ち (`dispatcher/shutdown.go`・`dispatchercmd.go`・`wake/wake.go`・`monitorcmd.go`) はループの中で間隔ごとに状態を見る形で、待ちだけを抜き出すと読みにくくなる。flock を使う残りの 6 module (pro-con の lock・presence・relay・flockmark・restartable・mac-lan-manager・serialrun) は待たずに一度だけ試す形か EINTR を取り直す形で、形が違う。`render.clean` / `main.clean` は `termsafe.PlainLine` の 1 行の別名。dupscan の L1m (`render.hasMsg` ~ glogx・`render.Count` ~ doctor) は別のドメインの小さい関数で寄せない (1065 と同じ判断) |
| dead-code | 1 (直した) | `subs.Load` / `subs.Save` が production から届かない (`deadcode`)。照合つきの `LoadVersion` / `SaveIf` に移った後、テストの準備だけで残っていた。`Save` は照合しないので、production から呼ぶと別の操作の変更を、知らせずに消す道になる → 消して、テストも照合つきの道で準備する (commit「refactor(slack-bbs): 照合しない subs.Save と subs.Load を消し、テストの準備も照合つきの道を通す」) |
| broken-code | 1 (直した) | CLI の `sub add`・`group create`・`group add` が、アーカイブ済みのチャンネルを入れられた。購読の画面は 1065 の直しで入れない (入れると読み直しのたびに「読めなかった」の注記が出続ける)。同じ約束を破る別の経路 (commit「fix(slack-bbs): CLI の sub add・group create・group add でも、アーカイブ済みのチャンネルを入れない」)。この直しは引いた時点で断ったので、購読済みのチャンネルが後からアーカイブされると、前は rc=0 だった `sub add #old` が rc=1 になった (敵対的レビューの指摘) → 新しく入れる操作だけを断る形に直した (commit「fix(slack-bbs): CLI で断るのはアーカイブ済みのチャンネルを新しく入れる操作だけにし、…」) |
| error-handling | 0 (記録 2) | 前回からの差分を opus の読み取りのみのレビューで見た。記録のみ: (a) `source.Client.LastPost` は slack-cli の stderr の警告を捨てるが、`history` の警告を 1 度出したことにする印 (`throttle.go` の `warningsShown`) を立てうる。列の状態のファイルが使えないとき、最終投稿の取り直しが板の読み直しより先に `history` を呼ぶと、板の読み直しの警告が出ない (列のファイルが使えない劣化した状態でだけ起きる。未確認) (b) 読む画面の購読の保存は、空の `from` を「d で外す保存」の印に使う。開いた後に `subscriptions.json` を手で消すと、`s` の画面からの保存が `ErrChanged` で断られ続ける (slack-bbs 自身は消さない)。`SaveIf` はファイルが無い・後から作られた・消えたの全部で断るか誤りを返す |
| concurrency | 0 (記録 1) | 記録のみ: 最終投稿のキャッシュ (`lastposts.json`) は、各画面が開いたときに読んだ分に自分が取った分を足してファイルごと書く。読む画面の `s` と別の端末の `slack-bbs sub` を同時に開くと、互いに相手が取った分を消す (K1。消えるのはキャッシュで、次に開いたときに取り直すだけ)。ほか: `tabsBase` は Update の goroutine だけが書く・最終投稿の結果は取り直しの実体のポインタで照合する・`callStats` は mutex で守る・`guard` の Add と Wait は競合しない |
| resource-leaks | 0 | `cmdSub` と `browse` は閉じたら取り消してから待つ。最終投稿の取り直しは閉じたときに `pickCancel` で止まる。`statusTick` は program と一緒に終わる |
| false-green | 0 | `channels` は一覧も購読も読めなければ rc=1。`sub rm` / `group rm` は 0 件なら rc=1、`sub add` の 0 件は rc=0 でそう書く、使い方の誤りは rc=2 |
| security | 0 | 端末へ出る外の文字列 (チャンネル名・グループ名・候補の名前・slack-cli の誤り・注記) は `clean` / `PlainLine` / `Label` / `report` を通る。`view.json` の Tab と Topic は照合にしか使わない。新しいキャッシュのファイル名は定数 |

受け入れた穴: CLI の `sub add` / `group add` は、チャンネルの一覧のキャッシュが無い (初回・壊れた・置き場が決まらない) と、ID で渡したチャンネルを確かめずに入れる。
一覧の取り直しは約 1 分かかるので、ID だけの引数では取らない、と決めた形 (`cli.go` の `resolveAll` のコメントと commit「fix(slack-bbs): CLI で一覧を取り直すのを 1 回までにし、…」)。
その代わり、アーカイブ済みかはキャッシュがあるときしか分からない。再提起するなら、キャッシュが無い状態で ID を渡して入れた実例を添える。
| ux | 1 (直した)・ユーザーの判断 1・記録 1 | 前回読まなかった範囲 (view の描画とキー・picker のキー処理・board/units.go・board/mrkdwn.go・view/highlight.go・view/list.go) を opus の読み取りのみのレビューで見た。直した: 購読の画面でグループを消すか確かめている間に、裏で取り直した一覧が届くと確認の板が知らせずに消え、確認のつもりの Enter が「保存して閉じる」になった (commit「fix(slack-bbs): 購読の画面でグループを消すか確かめている間に一覧の取り直しが届いても、確認を消さない」)。ユーザーの判断が要る: 購読の画面だけ `g` が「グループを作る」で、先頭へ移動できない (`G` は末尾へ動く)。`docs/glogx-ui-guide.md` §2 は `g`/`G` を全画面で端の移動とし、例外の記録が無い (`e` = エディタ・`p` = PR の語彙も同じ形)。記録のみ (未確認): 購読の画面を開いている間は、読む画面の `tiny()` の判定より先に picker の行を返すので、高さ 6 以下の端末では行が端末の高さを超える (bubbletea がどう切るかは確かめていない)。案内に出したキーが動くこと・幅を表示幅で引いていることは、読む画面・購読の画面とも見つからなかった |
| leaky-abstraction | 1 (直した) | 発言の行の頭の形 (字下げ 4・「(前) 」・時刻の書式) を render と読む画面の色付け (`view/highlight.go` の `messageHead`) が別々に持っていた。render の側だけを変えると、ビルドもテストも通ったまま名前と時刻の色が消える → render の定数に寄せた (commit「refactor(slack-bbs): 発言の行の頭の形 (字下げ・期間前の印・時刻の書式) を render の定数に寄せ、…」) |
| encapsulation | 1 (直した) | 「<時刻> に取った一覧・取り直し中」の注記を、`slack-bbs sub` (main.go) と読む画面の `s` (view.go) が別々に組んでいた → `Picker.SetCachedNote` に寄せた。ほか: 未読・隠したかの判定 (`topicUnread` / `topicHidden`) は list.go と read.go が同じ述語を使う。グループの名前の重複の判定は picker の `Press` と ops の `findGroup` の 2 か所にあるが、違いは「別のグループの ID と同じ名前」を通すかだけで、実害を示せない |
| mrkdwn (broken-code の一部) | 0 (記録 1) | 記録のみ (未確認): ラベルの無いチャンネルの記法 `<#C024BE7LR>`・ラベルが空の `<#C…|>` は ID のまま出る (`board.Plain` にチャンネルの名前を渡していない)。Slack が実際にこの形を送るかは実データで確かめていない。利用者の一覧に無い人 (Slack Connect の相手) へのメンションも `@U…` のまま。`@`・`!subteam`・`!here`・`!date`・`<url|label>`・`&amp;lt;` の二重のほどき・コードブロックの中の `<`・引用の `&gt;` は欠けも崩れもしない |
| performance | 1 (別 issue で直した) | 本文のスクロールのもっさりを実測し、tmux の中で同期出力を使っていたのを止めた ([1069](1069-perf-slack-bbs-sync-output-redraws-whole-pane-under-tmux.md)。外側へ送る量が j 1 回で約 199KB → 約 54KB) |
| design / responsibility / polymorphism / ui-components | 0 | `view/view.go` (約 1,400 行) は 1065 と同じ判断 (状態は `Model` 1 つに閉じ、分けて責務・探す手間・読む範囲が良くなる案は出せない)。新しく足した CLI (`cli.go`・`subs/ops.go`) は、一覧を変える純粋な関数 (ops) と、読み書きと出力 (cli) に分かれている |
| ci | 0 | `check_go_project_lanes.sh` が緑 (replace 依存 87 件の paths)。ctxwait を足した 7 つの workflow (slack-bbs・doctor・process_supervisor・ratelimit・zundamon-kaisetsu・glogx・pro-con) に `src/ctxwait/**` を足した |
| dependency | 0 | `go mod tidy -diff` は差分 0。ctxwait の取り込みで go.mod を直した module は、どれも `go mod tidy` の後に差分 0 |
| test-cleanup / test-helpers | 0 (直したもの 1) | 新しく足したテストの 1 本で、確認の板が出ているかを案内の行にも出る語で確かめていて、板が消えても通った (直す前の実装で通ったことで気づいた) → 板にだけ出る語で確かめる形にしてから commit した。テストの準備の helper (購読のファイルを書く) は、消した `subs.Save` の代わりに照合つきの道を通す形にした |
| lint-from-done | 0 | 今回の発見は、同じ約束を別の入口が破る形 (画面と CLI・作る側と読む側) で、字面の lint にできる形ではない |
| general | 0 | 上に含めた |

全数: 直した 7 (duplication 1・dead-code 1・broken-code 1・ux 1・leaky-abstraction 1・encapsulation 1、performance は 1069)、起票 0、ユーザーの判断 1 (`g` のキー)、記録のみ 6 (error-handling 2・concurrency 1・ux 1・mrkdwn 1・受け入れた穴 1)。

## 反証・敵対的レビュー (opus の読み取りのみのサブエージェント)

- ctxwait への寄せ: 旧実装と並べて、5 か所の書き換えに振る舞いの差が出る入力は作れなかった。取り込み漏れ 1 件 (pro-con が process_supervisor と
  ratelimit を通して ctxwait を使うのに、go.mod と CI の paths に無かった) を直した。block 形式の `replace ( … )` を数えていなかったのが原因
- 監査の直し 5 本 (Load/Save の削除・アーカイブ済み・確認の板・render の定数・注記): 退行 1 件 (上の broken-code) とテストの弱さ 1 件
  (render の行の色付けのテストが、名前の色を本文の色付けでも満たした) を直した。受け入れたもの: Refresh の後にグループの行の添字は構造上変わらないので、
  「ID で引き直す」を「元の添字を残す」に変えてもテストは通る (手で直したファイルに同じ ID のグループが 2 つあるときだけ違いが出る。板に名前が出るので y の前に気づける)

## ユーザーの判断が要るもの

- 購読の画面の `g` (グループを作る) は、`docs/glogx-ui-guide.md` §2 の「`g`/`G` は全画面で端の移動」と食い違う。選択肢: (a) `g` を先頭への移動に戻し、
  グループを作るキーを別にする (例: `n`・`+`) (b) slack-bbs の購読の画面の例外として、ガイドの §8 に記録する (c) 今のまま

## 進捗

- [x] 全観点 (2026-11-12)。直した 7・記録のみ 6・受け入れた穴 1
- [ ] `g` のキーの判断 (ユーザー)
