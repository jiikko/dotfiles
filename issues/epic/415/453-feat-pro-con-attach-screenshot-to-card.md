# 453 (feat): スクリーンショットをカードに添付して、PM / PG の Claude が見られるようにする

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-25): 「claude がスクショを確認できる場合はスクショもカード添付できるといいと思った」。
依頼や回答を文字でしか渡せないので、画面の不具合・見た目の依頼を伝えるには言葉で説明し直すしかない。Claude Code は画像を読める (Read で png / jpg)。

## 詳細 (期待する動作)

- 依頼・回答・差し戻し (446) にスクリーンショットを添えられる。添えた画像は、そのカードの PM / PG が読める場所に置き、PG への指示・再開の文にパスを載せる
- 口の候補: CLI `pro-con card attach <カード> <画像のパス>` / 画面の入力欄で画像のパスを貼る・クリップボードの画像を取り込む (macOS の `pngpaste` 等は要調査)
- カードの記録と `pro-con card show` に添付の一覧を出す

## 対応方針 (候補。着手のときに決める)

- 置き場: 状態の置き場の `attachments/<カード>/` (0700 / 0600。441 の「見せる範囲」と同じ扱い)。書き手は dispatcher だけ (426 の決定 1) を守るなら、
  受付の箱に画像ごと置いて dispatcher が移すか、画像は箱の外で書いて記録への載せだけ箱を通すかを決める
- 🚨 PG は自分の worktree で動き、`--setting-sources project,local` で起動している。状態の置き場 (~/.local/state/pro-con) の画像を Read できるか
  (権限・許可の設定) を実測してから決める。読めないなら worktree 側へ写す
- 大きさの上限と、カードを消す (451) / 片付ける (438) ときの後始末

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

