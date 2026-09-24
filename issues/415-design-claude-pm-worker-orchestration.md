# 415 (design): PM / PG 分離 — 依頼をタスクカードで追跡し、PG を自動スケーリングし、TUI で進捗を見る

起票日: 2026-09-24

## 概要

Claude Code の使い方を「session を立ち上げてそこで作業する」から、**要件を聞く PM と作業する PG を分ける**形へ移す。
人間は基本的に PM とだけ話し、PM が積んだタスクを PG が消化する。**人間の依頼はすべてタスクカードとして記録し、
どこへ行ったか分からなくなる状態を作らない**。PG の数は待ち時間を見て自動で増減させ、カードの進捗・溜まり具合・
タスクごとのログを TUI で見られるようにする。

## 背景 (いまの困りごと)

いまは session を PG に見立て、人間が各 session に直接作業させている。

- **session を行き来すると、その session が何をしていたか忘れる**。状態が人間の頭の中にしか無い
- **コンフリクトを人間がうっすら考えている**。どの session がどのファイルを触るかの管理が人間の仕事になっている
- 話しかける相手が N 個あり、切り替えのたびに文脈を思い出すコストがかかる
- Claude Code Desktop で複数 session をマルチウィンドウで立ち上げると、**全 session のウィンドウが常に表示されて邪魔**。PG は画面を持たず裏で動き、必要なときだけ前に出す形にしたい
- PM を挟むと、今度は **「PM に依頼した後、そのタスクがどこへ行ったか分からなくなる」** のが怖い

## 要件

1. **窓口は PM**。人間は PM に依頼し、PM がタスク (要件・受け入れ条件・触る予定のファイル) を書いてキューに積む
2. **PG の自動スケーリング**。キューの滞留 (件数・最古の待ち時間) を見て PG を起動し、キューが空になったら増やさない。上限を持つ
3. **タスクごとのログ**。PG はタスク単位でログを残し、後から読める
4. **TUI でキューの溜まり具合を表現する**。状態ごとの件数と最古の待ち時間が一目で分かる。タスクを選ぶとログを tail できる
5. **たまに PG と直接話せる**。TUI から該当 PG の session を対話で開ける
6. **コンフリクトを人間が考えなくてよい**。触る予定のファイルが重なるタスクは同時に走らせない。PG ごとに worktree を分ける
7. **依頼はタスクカードとして仕組みで管理する**
   - 人間が PM に依頼したら、PM は**作業に入る前に**カードを 1 枚作る (安定 ID / 依頼の原文をそのまま / 日時)
   - カードは作成から終了まで必ずどこか 1 つの状態に居て、担当 (PM / どの PG / 人間待ち) が分かる。状態が変わるたびに履歴へ追記する
   - 1 つの依頼を複数のタスクに分けた場合は、親子関係で元の依頼までたどれる
8. **進捗を TUI で視覚的に見る**。カンバン風に、カードを状態の列に並べる。カードを選ぶと、履歴とそのタスクのログが見られる
9. **TUI から btw 的な横入り質問ができる**。カードを選んで「今どうなってる？」と聞ける。**作業中の PG は止めず、その文脈も汚さない**
   (Claude Code の `/btw` と同じ性質。回答は PG の最新出力 (`claude logs <id>` 等) を元に、別プロセスで返す)
10. **カードと issue の紐づけを可視化する**。**PM への依頼が必ずしも issue になるわけではない**
    - カードは必ず作られるが、紐づく issue は 0 件のことも複数件のこともある (その場で回答した / 調べて終わった / 却下した)
    - issue に紐づかないカードは**終わり方**を必ず持つ: `回答済み` / `調査のみ` / `却下 (理由)` / `issue 化待ち`
    - 🚨 **`issue 化待ち` のまま放置されたカードは TUI で目立たせる** (依頼が行方不明になる典型的な形)
    - TUI はカードに紐づきのバッジ (`#415 #416` / `issue なし: 回答済み` / `⚠ issue 化待ち`) と、紐づく issue の状態 (open / next / done) を出す
    - 逆方向もたどれる: issue 側から、元になったカード (依頼の原文) を参照できる

### 不変条件

- **依頼は失われない**: カードの無い作業も、どの状態にも居ないカードも存在しない
- **すべてのカードは「紐づく issue を持つ」か「終わり方を持つ」のどちらか** (終了済みのカードについて)
- **カードが指す issue は実在する**: issue が done へ移動・改番されても切れない (パスでなく番号で参照する)
- これらは検査で確かめられる形にする (例: 状態の場所を全部数えた数 = カード総数 / 紐づく番号が issues/ 配下に実在する)

## ツールの形 (2026-09-24 に決めたこと)

- **名前は `pro-con` (仮)**。producer-consumer から。役割も **PM = producer / PG = consumer** と呼ぶ。
  ハイフン区切りは `bin/ci-log` / `bin/mutate-verify` と同じ流儀。PATH と Homebrew に同名なし (2026-09-24 に確認)
- **glogx のような TUI**。glogx からショートカットで開ける
- **TUI と常駐プロセスを分ける**。TUI (`pro-con`) を閉じても dispatcher (`pro-con daemon`) と PG は裏で動き続ける。
  TUI は状態ファイルを読み、指示を出すだけにする (何度開閉しても安全)。daemon は `lockman` で 1 つに限り、
  TUI 起動時に居なければ起こす (launchd で常駐させるかは未決)
- **人間は普段 Claude Code Desktop で session を動かしている**。tmux の `@claude_state` は Desktop の session を見られないので、
  段階 1 の見える化は transcript (`~/.claude/projects/*/*.jsonl`) を読む。Desktop の全 session が同じ形式で書かれるかは未実測

## 設計案 (未確定の叩き台)

```
人間 ⇄ PM (対話 session)
          │ ① 依頼を受けたら即カードを作る (原文・ID)
          │ ② 必要ならタスク / issue に分解してキューへ
          ▼
      カード + キュー (ファイル。状態は位置で表す)
          │ dispatcher (決定論的なスクリプト) が滞留を見て PG を起動
          ▼
      PG = claude --bg (1 タスク 1 session / 自分用の worktree)
          │ claude logs / agents で状態と出力を読む
          ▼
      glogx の PG 画面 (カンバン + ゲージ + ログ tail + attach + btw)
```

### 論点 1: スケーリングを誰が決めるか — dispatcher (機械) を推す

要望は「PM に連絡して、PM が待ち時間を考慮して PG を立ち上げる」。ただし判断そのものは
**決定論的な dispatcher に置き、PM (LLM) は「積む」と「上限の変更・即時の増員依頼」を行う**形を推す。
要望からの変更点なので、合意を取ってから決める。

- LLM に常時監視させると、判断のたびにトークンを使い、判断もぶれる。滞留件数・待ち時間・上限の比較は機械で足りる
- スケーリングの判断材料: キュー件数 / 最古の待ち時間 / 稼働中の PG 数 / 利用枠の残量 / **worktree のディスク使用量とビルドキャッシュの肥大** (PG 1 体ごとに worktree が増える)

### 論点 2: PG の形態 — `claude --bg` を第一候補にする

反証レビューで、Claude Code (2.1.281 で `claude --help` 実測) にネイティブの background session 管理があると分かった:
`claude --bg` (起動して id を返す) / `claude agents` (一覧) / `claude attach <id>` (対話で開く) / `claude logs <id>` (最近の出力) /
`claude stop <id>` (会話は残り、再度 attach 可) / `claude rm <id>` (安全なら worktree ごと削除) / `claude respawn`。

| 形態 | 利点 | 欠点 |
|---|---|---|
| **`claude --bg`** | 要件 5 (直接話す) を `attach` でそのまま満たす。一覧・ログ・停止が CLI にある。worktree の後始末も `rm` が持つ | 挙動は未実測 (下記) |
| タスクごとに `claude -p --output-format stream-json` | 起動・終了・完了判定が簡単。出力がそのままログになる | 実行中に割り込めない (終わってから `--resume` で開く) |
| tmux pane に常駐する対話 session | 途中で話しかけやすい | 何件もこなすと文脈が混ざり compaction で前タスクの前提が残る。自動化が send-keys 頼み |

**`--bg` を採る前に実測すること** (未実測。help の文面は契約ではない):
- 権限プロンプトが出たとき、bg session は止まって待つのか。それを `claude agents` から判別できるか (= `waiting` の検出に使えるか)
- `claude agents` / `logs` の出力は機械で読める形か (JSON 出力の有無)。完了と異常終了を区別できるか
- `rm` が消す worktree は `--bg` 自身が作ったものだけか (dispatcher が作った worktree を消さないか)

### 論点 3: カードとキューの置き場所

- **カードは issue と別物**として持つ (要件 10: issue にならない依頼がある)。repo 横断の置き場 (`~/.local/state/claude-pm/cards/`) に置き、
  issue へは `repo + 番号` で紐づける
- タスクの本文は各 repo の `issues/` に置く (claim は `next/` の symlink。既存の規約・hook・glogx の issues viewer が使える)
- 人間の依頼は複数 repo にまたがるので、カード側は repo 横断で 1 つの TUI に出す

### 論点 4: カードの状態遷移

`依頼 → 分解済み → queued → running → review (PM が diff と実行結果を読む) → done`。分岐は `waiting` (質問待ち) / `failed` と、
issue にならない終わり方 (`回答済み` / `調査のみ` / `却下` / `issue 化待ち`)。

- PG の「完了」は証拠にならない。**PM が diff と実行結果を読んでから done にする** (subagent-model-tiering の検閲と同じ扱い)
- `waiting` になったら PM か人間が答え、再びキューへ戻す

### 論点 5: 失敗モード (設計で吸収するもの)

| 失敗モード | 吸収の仕方 (案) |
|---|---|
| PG が途中で落ちる / マシン再起動 | 結果が無いまま session が居ない → `failed` にして worktree は残す。自動で再実行しない |
| **dispatcher 自体が落ちて誰も気づかない** (起動 0 のまま滞留) | TUI のゲージに dispatcher の生存 (最終 tick の時刻) を出し、古ければ警告する |
| dispatcher が 2 つ起動する | `lockman` で dispatcher 自体を排他する |
| **PG が権限プロンプトで止まる** | 許可するツール / permission mode を起動時に固定する。それでも止まったら `waiting` として検出する (検出方法は論点 2 の実測待ち) |
| **dispatcher を tmux pane から起動すると、PG が `TMUX_PANE` を継承する** | `_claude/hooks/tmux-pane-state.sh` は `TMUX_PANE` があれば `@claude_state` を書くので、PG の状態が起動元 pane のバッジを上書きする。PG の起動時に `TMUX_PANE` / `TMUX` を落とす |
| 利用枠 (5h / weekly) の枯渇・429 | 新規起動を止めて待つ。並列の上限は枠の残量で下げる。枠の読み方は未調査 |
| push の衝突 | PG は worktree で commit し `push origin HEAD:master`。失敗したら worktree を残して `review` に回す。自動 rebase はしない |
| 触るファイルの申告漏れで衝突 | 申告は目安であって保証ではない。衝突したら上と同じく `review` で PM が統合する |
| PM がカードを作り忘れる | 不変条件の検査で「カードの無い作業」を拾う (PG の起動はカード ID を必須にする) |
| PM を複数立てたときの採番衝突 | カード ID と issue の採番は 1 箇所に寄せる (この repo で既に 4 回衝突している。`.claude/rules/worktree-per-session.md`) |

### 論点 6: PM の数

最初の要望は「要件を聞く PM を 4 つ」。PM 同士が矛盾する要件を積む・同じ番号を取るので、**最初は PM 1 つで始め、
足すなら repo か領域で分ける**のを推す (PM を増やすと、いまの困りごとが 1 段上の PM 同士の調整に移るだけになる)。

### 論点 7: TUI

- glogx に PM / PG 画面を足す
  - **カンバン**: カードを状態の列に並べる。カードには紐づきのバッジ (要件 10) と担当を出す
  - **ゲージ**: 状態ごとの件数・最古の待ち時間・稼働中の PG 数 / 上限・dispatcher の生存
  - **詳細**: 選んだカードの依頼原文・履歴・紐づく issue の状態・ログ tail
  - **キー**: attach (新しい tmux window で `claude attach <id>`) / btw (要件 9)
- 🚨 **見た目は本体に入れる前にサンプルレンダラで決める** (`decide-layout-in-sample-renderer-first.md`)。
  カンバンは列が複数並ぶので、サンプルでも複数列・複数カードで描く
- 既存の `@claude_state` (`scripts/tmux_agent_panel.sh` / `scripts/tmux_agent_jump.sh`) は tmux pane を持つ対話 session の状態。
  `--bg` の PG は pane を持たないので、PG の状態は `claude agents` 側から読む

## 段階

1. **見える化だけ先に作る**。いまの使い方 (対話 session を PG に見立てる) のまま、session ごとの担当 issue・最後の発言・状態を一覧する。
   「何をしていたか忘れる」はこれだけで大半が消える
2. `claude --bg` の実測 (論点 2) と、カードの形式・不変条件の検査
3. PM を 1 つ置き、カードとキューと dispatcher (上限固定・手動起動) を入れる
4. 自動スケーリング (滞留と枠の残量で起動数を決める)、カンバン・btw

## 関連ファイル

- `scripts/tmux_agent_panel.sh` / `scripts/tmux_agent_jump.sh` — 対話 session の状態 (`@claude_state`) の一覧とジャンプ
- `_claude/hooks/tmux-pane-state.sh` — `@claude_state` を書く hook (`TMUX_PANE` があるときだけ書く)
- `src/glogx/` — TUI 本体。issues viewer の契約は `docs/issues-viewer-spec.md`
- `bin/lockman` — dispatcher の排他に使う候補
- `docs/claude-fork-popup.md` — 並走 session を popup で覗く試み (休眠中。使いにくかった理由が書いてある)

## 進捗

- [x] 反証レビュー (sonnet 1 体、観点 3 つ)。採用 5 件:
  - P1: `claude --bg` 系を論点 2 の比較から漏らしていた → `claude --help` で実在を確認し、第一候補にして未実測の項目を列挙
  - P2: dispatcher 自体の停止 → 失敗モードに追加
  - P2: 権限プロンプトで止まるリスクが失敗モード表に無い → 追加
  - (追加指摘) PG が `TMUX_PANE` を継承して起動元 pane の `@claude_state` を上書きする → `_claude/hooks/tmux-pane-state.sh:60` で確認し、失敗モードに追加
  - P3: worktree のディスク使用量 → 論点 1 の判断材料に追加
  - 反証できなかった主張: 参照ファイルの実在と役割、`@claude_state` が pane の無い PG に乗らないこと、`-p` / `--resume` の存在
- [x] 要件 7〜10 (タスクカード / カンバン / btw / issue との紐づけ) を追記
- [ ] 論点 1〜7 の決定 (論点 1 のスケーリングの判断主体は要望からの変更なので合意が要る)
- [ ] `claude --bg` の実測 (論点 2 の 3 項目)
- [ ] 段階 1 のサンプルレンダラ
