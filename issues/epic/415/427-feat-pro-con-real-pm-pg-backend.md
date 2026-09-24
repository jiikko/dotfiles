# 427 (feat): pro-con の本物の PM / PG の backend (段階 3〜5)

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md) の「段階」3〜5

## 概要

模擬 (`src/pro-con/fake`) で UI とつなぎ込みを確かめた機能を、本物の Claude Code の session で動かす。
[425](done/425-research-claude-bg-remaining-measurements.md) の実測と [426](done/426-design-pro-con-open-decisions.md) の決定は済んだ (2026-09-24)。

## 範囲 (段階ごとに分けて入れる)

- [ ] 段階 3: 受付 PM 1 つ + カードとキュー + dispatcher (上限固定・手動起動)。質問への回答 UI と watchdog もここで入れる
- [ ] 段階 4: リソースの直列化 (PG が 2 体以上になると要る)
- [ ] 段階 5: 自動スケーリング (滞留と枠の残量で起動数を決める)

## 前提

- **設計の決定は 426 の「決定」節が正本** (カードの書き手は daemon / 回答は stop → resume / 追記は turn の区切り / 落ちたら自動の再開 + 回数で止める /
  役割 4 つ (PG・テストの係・調べる係・レビューの係) / PM 1 つ / 知らせは tmux の status + macOS の通知)。ここには写さない

- **PG / 係を起動したら、`live.Register` で pro-con の記録 (`$XDG_STATE_HOME/pro-con/live/sessions.json`) に足す**。本物のモードは記録にある session
  だけを扱う ([424](424-feat-pro-con-readonly-real-backend.md) の範囲の変更)。記録の書き手は daemon だけ (426 の決定 1)
- **記録には session id と pid が必須** (`live.Register` が拒む)。`claude --bg` が返す短い id で `claude agents --json` を引き、session id と pid を得てから記録する。
  再開のときも CardID を渡す (書き直しは行ごと置き換える)
- **pid は落ちて自動で再開されると変わる** (425)。変わると pro-con の session は一覧から外れる (失敗側)。自動の再開 (transcript に
  「automatically restarted」の注記が入る) なら新しい pid で記録を書き直し、外での再開なら書き直さない、を区別する
- **外からの操作を検出する**: pro-con の session は、他の shell からも `claude attach` / `claude stop` できてしまう (Claude Code の側で止められない)。
  pro-con が指示していない変化 (止まった・入力が増えた・transcript に pro-con の知らない人間の発言) を見つけたら、カードに「外から操作された」と出して止める
- PG / 係は [431](431-feat-pro-con-pg-session-settings.md) の役割ごとの設定で起動する (431 を先にやる)
- 模擬 (ハリボテ) は残して起動のときに選ぶ。起動の口・状態ファイルの分け方・画面の区別は [424](424-feat-pro-con-readonly-real-backend.md) の「模擬と本物の併用」節

### 415 の決定事項

- PG は `claude --bg -w` で起動し、自分のブランチまで push する (master へは PM がレビューしてから)
- 同時実行数の上限は 2 から始める
- dispatcher と watchdog は決定論的に作る (LLM に見張らせない)

## 実測で分かった前提 (425)

- PG の状態は `claude agents --json --all` の `status` / `state` / `pid` / `waitingFor` で読める (完了・API エラー・停止・プロセスの死・権限待ち・質問待ちを区別できる)
- PG への送信は SendMessage (bg session も `ListAgents` に名前で出る)。枠は `claude -p "/usage"`
- PG にもユーザーの hook と規約が効く (起動時 約 13 万 token。Stop hook が別の作業の issue を更新させにいく)。PG 用の `--settings` で hook を絞るかを決める

## 関連ファイル

- `src/pro-con/backend/backend.go` (UI との口。模擬と同じ interface を満たす)
- `src/pro-con/fake/` (振る舞いの手本)

## 進捗

- [ ] 着手できる (425 の実測・426 の決定が済んだ。2026-09-24)
