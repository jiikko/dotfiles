# 453 (feat): スクリーンショットをカードに添付して、PM / PG の Claude が見られるようにする

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

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

