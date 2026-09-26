# 453 (feat): スクリーンショットをカードに添付して、PM / PG の Claude が見られるようにする

起票日: 2026-09-25

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-25): 「claude がスクショを確認できる場合はスクショもカード添付できるといいと思った」。
**意図は「PG がスクショを撮ってカードに添付し、人間が見られる」** (同じ日にユーザーが訂正: 「僕が伝えたかったのは、pg がスクショを添付して人間が見えたいっていう話だった」)。
最初の起票は逆向き (人間が画像を添えて PG が見る) に書いていたので、書き直した。そちらで起こしたカード C-028 は PG が起動する前に削除した。

## 詳細 (期待する動作)

- **PG が添付する**: 作業の証拠 (作った画面の見た目・コマンドの出力・前後の比較) を撮って、`pro-con card attach <カード> <ファイル> [--note <一言>]` でカードに付ける。
  PG への指示 (`dispatcher.Prompt`) に「見た目を変えたら撮って添付する」を足す
  - 撮り方の候補 (着手のときに決める): TUI は隔離した tmux (`-L`) の `capture-pane -e` (色つきの文字) / 画像にするなら vhs・freeze などの道具。
    GUI のアプリは macOS の `screencapture -l <window>` (画面収録の許可が要る。PG は bg なので使えるかを実測する)
- **人間が見る**: カードの詳細 (enter) に添付の一覧 (撮った時刻・一言・種類) を出し、キーで開く (画像は `open` で Preview、文字の画面は詳細の中にそのまま出す)。
  `pro-con card show` にも添付の一覧とパスを出す。`--view` でも見られる (読むだけ)
- **レビューする側 (取り込みの係・PM の Claude)** は添付の画像のパスを Read して、見た目のレビューに使える
- 置き場: 状態の置き場の `attachments/<カード>/` (0700 / 0600)。書き手は dispatcher だけ (426 の決定 1): 箱に「添付」の依頼とファイルを置き、dispatcher が移して記録に載せる。
  大きさの上限と、カードを消す (451) / 書庫へ移す (478) ときの後始末を決める

## 逆向き (人間が画像を添えて PG が見る) は別の話として残す

- 7d の実測 (2026-09-25): PG と同じ条件 (`-p`・既定の許可・`--setting-sources project,local`・worktree の cwd) では、状態の置き場の png は許可が要って拒まれる。
  `--add-dir <attachments>` を足すと読めた。`--bg` は未測定。人間から PG へ渡す向きを作るときはこれを使う

## 関連

- 446 (差し戻し) / 451 (削除のときの後始末) / 441 (viewer。外の Claude が添付を見るか)

## 実測: PG と同じ条件の session が、状態の置き場の png を Read できるか (2026-09-25 dotfiles-7d)

条件: Claude Code 2.1.282・haiku・`claude -p --setting-sources project,local --settings '{"disableAllHooks":true}' --output-format json`
(許可のモードは既定 = PG の `claude --bg -w … --setting-sources project,local` と同じ)。cwd は使い捨ての repo の `.claude/worktrees/pc-c-001`
(PG と同じ置き方の git worktree)。png は `…/state/pro-con/live/attachments/C-001/shot.png` (0700 / 0600) に置いた。判定は `permission_denials`。

| | png の場所 | 許可の設定 | 結果 |
|---|---|---|---|
| A | 状態の置き場 (cwd の外) | 無し | **拒まれた** (`permission_denials` に Read。-p は聞けないので拒否。--bg ならここで許可のプロンプトを出して止まる) |
| B | worktree の中 | 無し | 読めた |
| C | 状態の置き場 | 起動に `--add-dir <attachments>` | 読めた |
| D | 状態の置き場 | 本体 (repo の root) の `.claude/settings.local.json` に `Read(/<attachments>/**)` | 読めた (worktree は本体の中の .claude/worktrees/ にあり、本体の local の設定が効く) |
| E | 状態の置き場 | D と同じ、cwd は本体 | 読めた (D の対照) |

- dotfiles では、本体の `.claude/settings.local.json` に `Read(//Users/koji/**)` があるので、PG は `~/.local/state/pro-con` を読める見込み (D の形)。
  ただしそれは追跡しないユーザーの手元の設定頼みで、ほかの repo では A になり、PG が許可のプロンプトで止まる
- 🚨 **`--bg` では測れていない** (隔離した cwd では「Workspace not trusted」で起動を拒む。信頼を与えるには `~/.claude.json` に書く必要があり、しなかった)。
  -p で拒まれた形が --bg で「許可のプロンプトで止まる」かは推定
- haiku が答えた画像の大きさ (100 x 100) は実物 (37 x 21) と違った。「読めた」の根拠は拒まれなかったこと (`permission_denials` が空) まで

### 置き場の決め方への材料

1. **起動 (と再開) に `--add-dir <状態の置き場>/attachments/<カード>` を足す**: 許可の設定に頼らず読めた (C)。置き場は状態の置き場のまま (0700)、PG ごとにそのカードの分だけ見せられる
2. **添付を PG の worktree の中へ写す**: 許可は要らない (B) が、worktree の git status に出る (.gitignore が要る)・写す手間と後始末が増える
3. 許可の設定 (settings.local.json の Read の許可) に頼る: repo ごと・ユーザーごとに違い、無い repo で止まる。既定にしない
→ 1 を推す (--bg の A-B は、信頼済みの repo の worktree で取れるときに確かめる)

## 実装 (2026-09-26 C-030)

- `pro-con card attach <カード> <ファイル> [--note <一言>]`: ファイルを受付の箱の `inbox/files/<依頼の ID><拡張子>` に写してから依頼を置く。
  dispatcher (`store.Apply`) が記録に当てられると決めてから `attachments/<カード>/<依頼の ID><拡張子>` (0700 / 0600) へ移し、
  `card.Attachment` (パス・元の名前・一言・種類・大きさ・時刻) を記録に載せる。
  置き場の名前は依頼の ID と拡張子だけで決める (依頼に書かれた名前・パスは使わない)。移した後・記録を書く前に落ちたら、次の Apply が移し先を見て当て直す
- 種類は拡張子で決める: 画像 (png / jpg / gif / webp / heic / pdf) は `o` で `open -a Preview` へ、文字 (txt / ans / log / out) は詳細の中に出す。
  それ以外 (ファイル) は開かずにパスだけ見せる (.terminal / .webloc のような開くと動くファイル・ブラウザで JS が動く .svg を、人間の権限で o から動かさない)
  (色 = SGR は残し、ほかの制御は `termsafe.DetailLine` で落とす。256 KiB / 400 行まで)。`card show` は一覧とパス (消えていればそう書く)。`--view` でも見られる
- **大きさの上限**: 1 件 20 MiB (Retina の全画面の png が 10 MiB 前後)・1 枚に 50 件。付ける側 (CLI) で先に断り、dispatcher でも見る。完了のカードには付けない
- **後始末**: dispatcher が毎 Tick、記録に無いカード (削除した = 451 / 書庫へ移した = 478) の `attachments/<カード>/` を消す
  (記録を書いた後に消すので、落ちても次の Tick で消し直す)。依頼の来ない `inbox/files/` のファイルは 1 時間で消す。除けた依頼のファイルはその場で消す。
  → **書庫へ移したカードの添付は残らない** (`card show` は「ファイルが無い」と出す)。レビューは完了の前に終わっている前提
- PG への指示 (`dispatcher.Prompt`) に「画面の見た目を変えたら撮って attach する」と撮り方を足した。PM の指示書にも「添付は card show のパスを Read」を足した
- 一言と元の名前は記録に入れる前に `termsafe.PlainLine` を通す (履歴・詳細・card show にそのまま出る)
- 敵対的レビュー (2026-09-26、読むだけのサブエージェント) の指摘のうち、再現条件の具体的な 3 件を直した:
  o が画像以外 (.terminal 等) も open に渡す / 一言のエスケープが履歴に出る / 箱に手で置いた `*.json` を除けるとき、名前が glob になって他の依頼の添付を消す。
  直さずに残したもの (どれも同じユーザーが手で仕込む前提): `attachments/<カード>` や `inbox/files` を symlink にされるとリンク先へ移す /
  適用済みの依頼を控え (1000 件) から外れた後に同じ名前で置き直すと添付が 2 件になる。引き出しを描くたびに文字の添付を切り直す費用は未測定
- 🚨 未確認: レビューする側 (PM・取り込みの係の Claude) が worktree の cwd から状態の置き場の png を Read できるかは、上の実測 (7d) の D の形
  (本体の `.claude/settings.local.json` の Read の許可) 頼み。無い repo では許可のプロンプトで止まる

### 撮り方の実測 (2026-09-26、bg の session から)

| 方法 | 結果 | 採否 |
|---|---|---|
| 隔離した tmux (`-L`) の `capture-pane -e -p` | 色つきの文字 (SGR) がそのまま取れる。道具が要らない (tmux は e2e モードでも使っている) | **TUI の既定**。.ans で付け、詳細の中にそのまま出す |
| vhs 0.10.0 の `Screenshot` | bg から約 2 秒で PNG (600x200) を作れた | 画像が要るとき (Claude に画像で見せる・人に Preview で見せる)。入っていない環境もある |
| freeze / termshot | 入っていない | 使わない |
| `screencapture -x` | rc=0 で返るが、中身は壁紙とメニューバーだけ (窓が写らない = bg に画面収録の許可が無い) | **使わない** (成功に見えて証拠にならない)。GUI のアプリの撮り方は未解決 |

