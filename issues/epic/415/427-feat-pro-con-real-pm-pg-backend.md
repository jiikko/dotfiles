# 427 (feat): pro-con の本物の PM / PG の backend (段階 3〜5)

> 🚨 **担当中: dotfiles-5c**（2026-09-24〜）

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md) の「段階」3〜5

## 概要

模擬 (`src/pro-con/fake`) で UI とつなぎ込みを確かめた機能を、本物の Claude Code の session で動かす。
[425](done/425-research-claude-bg-remaining-measurements.md) の実測と [426](done/426-design-pro-con-open-decisions.md) の決定は済んだ (2026-09-24)。

## 範囲 (段階ごとに分けて入れる)

- [ ] 段階 3: 受付 PM 1 つ + カードとキュー + dispatcher (上限固定・手動起動)。質問への回答 UI と watchdog もここで入れる
- [ ] 段階 4: リソースの直列化 (PG が 2 体以上になると要る)
- [ ] 段階 5: 自動スケーリング (滞留と枠の残量で起動数を決める)

## 段階 3 の小分け (2026-09-24 夜に決めた。上から順に進め、済んだら [x] と commit を書く)

- [x] **3a カードの置き場所** (`src/pro-con/store`): daemon だけが書くカードの記録 (`$XDG_STATE_HOME/pro-con/live/cards.json`) と、受付の箱
  (`…/live/inbox/` に 1 件 1 ファイルの依頼)。PM / PG / 画面は箱に置くだけで、適用は daemon の 1 か所 (426 の決定 1)。
  採番と `card.Check` の不変条件の検査も適用の中。枠を使わず単体テストで確かめられる
  - 済み (2026-09-25): `src/pro-con/store` (Submit / Load / Apply)。依頼の種類は add / plan / ask / answer / review / close
    (分解済み → 作業中は daemon の仕事なので 3c)。遷移の規則は `transition` の 1 か所、不変条件は「新しく出た違反」で判定。
    テスト 6 本、変異 4 本が red (二重適用の控え / 不変条件 / 質問待ちでない回答 / 壊れた記録を空と読む)。
    🚨 **敵対的レビューは未実施** (週の利用枠が 96% のため見送り)。二重適用の防止と「書き手は daemon だけ」の排他 (3c) を、枠が戻ったら攻めさせる
- [x] **3b `pro-con card` コマンド**: PM / PG が使う口 (`add` 依頼を積む / `ask` 質問を書く / `done` 終えた / `plan` 分けた 等)。箱に置くだけ。枠を使わない
  - 済み (2026-09-25): `src/pro-con/cardcmd.go`。置いた依頼の ID を stdout、使い方の誤りは箱に置く前に rc=2。
    テスト 3 本、変異 2 本が red (--issue の書式の検査 / add の必須の検査)
- [ ] **3c `pro-con daemon`**: 箱の適用 → PG の起動 (`claude --bg -w`、431 の設定、`live.Register`) → `claude agents --json` で状態を読んでカードへ →
  回答・追記は stop → resume (426 の決定 2・3) → 落ちた回数で止める (決定 4) → watchdog。起動の部分は本物の claude が要る (枠を使う)
  - [x] 3c-1 分解済みのカードに PG を起動 (上限まで)・記録 (`live.Register`) に登録・回答を受けたカードは stop → resume。起動の口は差し替えられる形にし、偽物で単体テスト (枠を使わない)
    - 済み (2026-09-25): `src/pro-con/daemon` (Tick = 箱の適用 → 一覧に出た PG の登録 → 分解済みへの割り当て)。回答は card.Resume に入り、
      再開で渡したら空にする。起動の口は Launcher で差し替え、本物は ExecLauncher (`claude --bg -w … --setting-sources project,local`)。
      🚨 **ExecLauncher は本物の claude で一度も走らせていない** (3f で確かめる)。store に daemon 用の Update を足した (不変条件の検査は Apply と共通)。
      テスト 6 本、変異 3 本が red (上限 / 再開の分岐 / 起動の失敗で作業中にしない)。敵対的レビューは未実施 (枠のため)
    - 登録の穴を塞いだ (2026-09-25): 記録を書き直すのは daemon 自身が起動・再開した直後 (pending) だけ。それ以外で pid が変わったら
      「外から操作された疑い」を知らせて書き直さない。最初の登録も、session の起動がカードの起動より前なら取り込まない。変異 2 本が red。
      🚨 pending は daemon のメモリの中だけ。daemon が起動し直すと失うので、その間に Claude Code が自動で再開した PG は「外から操作された疑い」に倒れる (3c-2 で扱う)
  - [ ] 3c-2 落ちた回数で止める (426 の決定 4)・watchdog (停滞)
  - [ ] 3c-3 `pro-con daemon` の常駐と排他 (2 つ起動しない)
- [ ] **3d 画面**: 本物の backend が 3a の記録を読み、書き込み (回答・依頼 等) は箱に置く。読み取り専用をやめる
- [ ] **3e PM への指示書**: PM の session に渡す、`pro-con card` の使い方と規律 (AskUserQuestion を使わない 等)
- [ ] **3f 本物の claude で通しの確認** (枠を使う。424 から引き継いだ受け入れ条件もここ)

## 前提

- **設計の決定は 426 の「決定」節が正本** (カードの書き手は daemon / 回答は stop → resume / 追記は turn の区切り / 落ちたら自動の再開 + 回数で止める /
  役割 4 つ (PG・テストの係・調べる係・レビューの係) / PM 1 つ / 知らせは tmux の status + macOS の通知)。ここには写さない

- **PG / 係を起動したら、`live.Register` で pro-con の記録 (`$XDG_STATE_HOME/pro-con/live/sessions.json`) に足す**。本物のモードは記録にある session
  だけを扱う ([424](done/424-feat-pro-con-readonly-real-backend.md) の範囲の変更)。記録の書き手は daemon だけ (426 の決定 1)
- **記録には session id と pid が必須** (`live.Register` が拒む)。`claude --bg` が返す短い id で `claude agents --json` を引き、session id と pid を得てから記録する。
  再開のときも CardID を渡す (書き直しは行ごと置き換える)
- **pid は落ちて自動で再開されると変わる** (425)。変わると pro-con の session は一覧から外れる (失敗側)。自動の再開 (transcript に
  「automatically restarted」の注記が入る) なら新しい pid で記録を書き直し、外での再開なら書き直さない、を区別する
- **外からの操作を検出する**: pro-con の session は、他の shell からも `claude attach` / `claude stop` できてしまう (Claude Code の側で止められない)。
  pro-con が指示していない変化 (止まった・入力が増えた・transcript に pro-con の知らない人間の発言) を見つけたら、カードに「外から操作された」と出して止める
- PG / 係は [431](431-feat-pro-con-pg-session-settings.md) の役割ごとの設定で起動する (431 を先にやる)
- 模擬 (ハリボテ) は残して起動のときに選ぶ。起動の口・状態ファイルの分け方・画面の区別は [424](done/424-feat-pro-con-readonly-real-backend.md) の「模擬と本物の併用」節

### 415 の決定事項

- PG は `claude --bg -w` で起動し、自分のブランチまで push する (master へは PM がレビューしてから)
- 同時実行数の上限は 2 から始める
- dispatcher と watchdog は決定論的に作る (LLM に見張らせない)

## 実測で分かった前提 (425)

- PG の状態は `claude agents --json --all` の `status` / `state` / `pid` / `waitingFor` で読める (完了・API エラー・停止・プロセスの死・権限待ち・質問待ちを区別できる)
- PG への送信は SendMessage (bg session も `ListAgents` に名前で出る)。枠は `claude -p "/usage"`
- PG にもユーザーの hook と規約が効く (起動時 約 13 万 token。Stop hook が別の作業の issue を更新させにいく)。PG 用の `--settings` で hook を絞るかを決める

## 受け入れ条件 (424 から引き継ぎ)

- [ ] pro-con が起動し、記録 (`sessions.json`) に書いた本物の bg session が、本物のモードのカードに出る (424 では記録を書く側が無く、実物で確かめていない)

## 関連ファイル

- `src/pro-con/backend/backend.go` (UI との口。模擬と同じ interface を満たす)
- `src/pro-con/fake/` (振る舞いの手本)

## 進捗

- [ ] 着手できる (425 の実測・426 の決定が済んだ。2026-09-24)
