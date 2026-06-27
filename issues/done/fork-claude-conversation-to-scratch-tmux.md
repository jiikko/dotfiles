# 引き継ぎ: Claude 会話を fork して detached tmux セッションに退避し scratch popup で見る

> このセッション (Claude Code session id `bce90ef8-4f2a-49c0-8747-0226b7f113a9`) で設計が固まったが、
> context window 圧迫のため未実装で引き継ぐ。別セッションがこの資料だけで実装を継続できるように、
> 決定事項・**検証済みの技術的事実**・実装プラン・未解決点をまとめてある。

## ゴール（やりたいこと）

**Claude の会話の中で skill/コマンドを実行 → 今の会話を `--fork-session` でフォーク → フォークを
detached な tmux セッションに `claude` として作成 → prefix キーの popup トグルで scratch 風に表示する。**

- 「今の作業会話の文脈を引き継いだ別 Claude」を、元会話を汚さずに枝分かれさせ、フローティング
  popup で覗ける。フォークは独立して進み、永続（後から resume 可）。
- 公式 `/btw`（会話内で打つ quick-aside コマンド）とは別レイヤー。名前は衝突回避でずらす
  （例 `/fork-scratch` など。要決定）。

## 決定事項（このセッションのユーザー回答）

1. **フォークの開き方 = detached セッション + popup で見る**（新 window でも新セッション切替でもなく）。
   - 理由: フォークを detached な tmux セッションに作り、`prefix + <key>` の popup トグル
     （scratch/bind t と同型）で表示する。作業画面を隠さず重ねられる「scratch の見た目」。
2. **btw-tmux（方向A）は不採用 = 削除済み**。
   - 「端末でキーを押すと常駐スクラッチ Claude が popup で出る」(tmux→claude) を一度実装したが、
     ユーザーが本当に欲しかったのは逆向き(会話内の skill→fork)だったため `bind b` と
     `scripts/tmux_btw_launch.sh` を revert・削除済み（再実装しないこと）。

## 検証済みの技術的事実（再導出不要・実機 tmux 3.5a / macOS / claude CLI v2.1.193）

- **現在のセッション ID は env で取れる**: `CLAUDE_CODE_SESSION_ID`（このセッションでは
  `bce90ef8-4f2a-49c0-8747-0226b7f113a9`）。Claude の Bash tool が起動するシェルの env に入っている
  ので、会話内のコマンドから `$CLAUDE_CODE_SESSION_ID` で参照できる。
- **fork の起動コマンド**: `claude --resume "$CLAUDE_CODE_SESSION_ID" --fork-session`
  - `--resume <id>` で履歴を読み込み、`--fork-session` で **新しいセッション ID に枝分かれ**
    （元 ID 温存）。help 原文: 「--fork-session: When resuming, create a new session ID instead of
    reusing the original (use with --resume or --continue)」。
  - 元会話が生きたままでも `--fork-session` なら新 ID なので競合しない（読み込み+分岐）。
- **`claude --continue` は cwd スコープ**（help: 「Continue the most recent conversation in the
  current directory」）。専用 dir で動かせばその dir の会話だけを再開できる（fork とは別用途だが参考）。
- **`display-popup` は呼び出し元をブロックする**。会話内（Claude の Bash tool）から直接ブロックする
  popup を出すのは相性が悪い。→ **フォークは detached セッションに作り、別の popup キーで表示**する構成。
- **popup overlay 再描画バグ #4920（tmux 3.5a）**: popup を閉じた後にボーダーが ~1秒残る/もっさり。
  修正は未リリースの 3.7 / `brew install tmux --HEAD` のみ（stable 3.6b には未収録・実機 CHANGES で確認）。
  回避: 閉じた直後に `refresh-client` を叩く。既存 `scripts/tmux_refresh_all_clients.sh` を流用可。
- **`new-session -d -A` を既存セッションに打つと「閉じるのに prefix を2回押す」回帰**になる
  （実測で原因特定済み）。→ popup 起動は **`has-session ... || new-session`** ガードにすること
  （素の `new-session -A` を使わない）。
- **scratch popup の確立した作法**（`_tmux.conf` の `bind t` がお手本。これを fork ビューアにミラーする）:
  ```
  bind <key> if-shell -F '#{==:#{session_name},<sess>}' \
    "detach-client ; run-shell -b '${DOTFILES_DIR:-$HOME/dotfiles}/scripts/tmux_refresh_all_clients.sh'" \
    "display-popup -E -w 80% -h 75% -b heavy -S <枠style> -s <内側style> -T '<title>' \
     'unset TMUX; tmux has-session -t <sess> 2>/dev/null || tmux new-session -d -s <sess> <cmd>; exec tmux attach -t <sess>'"
  ```
  - 閉じ側（THEN）は `detach-client ; run-shell -b <refresh>`（`;` 区切り。`\;` でなく `;` が正）。
  - `${DOTFILES_DIR:-$HOME/dotfiles}` を含む path は run-shell の単一引用符内（または set-hook の
    `\$` エスケープ）で /bin/sh に解決させる。二重引用符内に裸で書くと tmux が `${VAR:-default}` を
    展開できず "invalid environment variable" になる（config 既存コメント参照）。
  - 条件分岐 `#{?...}` の中の `#[...]` 内カンマは `#,` でエスケープ必須（未エスケープでスタイル破損）。
    単一属性 `#[fg=x]#[bg=y]` に分ければカンマ自体を避けられる。

## 実装プラン

### 1. フォーク投入コマンド/skill（Claude 会話側）

- 配置: dotfiles の claude 設定管理下に置く。**現状 `~/.claude/commands/` は空・`commands` ディレクトリ
  運用なし**。skills は `~/dotfiles/_claude/skills/`（→ `~/.claude/skills/` に symlink 運用）。
  → slash command を使うなら `_claude/commands/<name>.md` を作り symlink を張る運用を確認・新設する。
  skill にするなら `_claude/skills/<name>/SKILL.md`。**slash command（明示 `/name` 実行）の方が
  「明示的に fork する」UX に合う**（skill は description 自動発火向き）。要判断。
- 動作: コマンド本文で Claude に次の Bash を実行させる（`$CLAUDE_CODE_SESSION_ID` は Bash env にある）:
  ```
  tmux new-session -d -s <fork-sess> "claude --resume \"$CLAUDE_CODE_SESSION_ID\" --fork-session"
  ```
  detached（`-d`）なので非ブロッキング。作成後ユーザーに「popup キーで開ける」と案内。
- **セッション名 `<fork-sess>` の設計（未決・下記「要検討」）**: 固定名 か 一意名 か。

### 2. popup ビューア（tmux 側 / `_tmux.conf`）

- `prefix + <key>` で `<fork-sess>` を popup トグル（上記 scratch 作法をミラー）。
- 固定名なら単純トグル。一意名（複数フォークを溜める）なら「どのフォークを開くか」を
  fzf popup で選ぶビューアが要る（`scripts/tmux_fzf_jump.sh` が `list-windows -a` + capture-pane
  プレビューの fzf popup の実例。これを `list-sessions` フィルタ版にすればよい）。
- 枠色は scratch（青×ピンク×濃紺）と区別（例: 別色）。

## 要検討（実装前に決める）

1. **フォークセッション名**: 固定（例 `claude-fork`。常に1つ・上書き運用）か、一意
   （例 `claude-fork-<元id短縮>` や時刻。複数フォークを溜める）か。
   - 固定 → popup は単純トグルで済む。
   - 一意 → 複数保持できるが、popup ビューアに fzf 選択が必要。
2. **skill か slash command か**、および dotfiles での配置（`_claude/commands/` 新設 or skill）。
   公式 `/btw` と衝突しない名前（`/fork-scratch` 等）。
3. **transcript flush タイミング**: フォークが「今この瞬間まで」を含むか。直近ターンが未 flush の
   可能性があり、`--fork-session` が拾う履歴の鮮度を実機確認（fork 直後に最新発言が入っているか）。
4. **初回 fork セッションの claude 起動確認**（実機 / interactive なので headless 不可）。

## 関連ファイル

- 流用: `scripts/tmux_refresh_all_clients.sh`（#4920 の refresh 回避。閉じ側で使う）、
  `_tmux.conf` の `bind t`（scratch popup の型）、`scripts/tmux_fzf_jump.sh`（fzf popup セレクタの実例）。
- 新規予定: フォーク投入コマンド（`_claude/commands/<name>.md` or skill）、popup ビューアの bind、
  （一意名なら）fork セレクタスクリプト。
- 削除済み（不採用・再実装しない）: `scripts/tmux_btw_launch.sh` + `_tmux.conf` の `bind b`。

## このセッションの周辺成果（参考）

- `docs/tmux-as-platform.md`: 「tmux を小さなツールの土台として使う」実用 doc を作成済み。
  **未コミット**（push 待ち）。tmux でできること（UI primitive / 自動化 / ダッシュボード）と
  この repo の実例マップ、アイデア集、版注意（#4920 等）を含む。fork 機能の文脈把握にも有用。
- 直近コミット済みの tmux 改修: scratch popup の点滅/2色枠/濃紺内側、閉じる時の2回押し回帰修正、
  popup 閉じ後の枠残骸 refresh 回避、アクティブ pane 濃紺背景化。

## メモ

- 本資料はバグ issue ではなく設計引き継ぎなので codex レビューは省略（断定/カウントを多用していない）。
  実装に着手する別セッションは、着手前にこの「要検討」を1つずつ潰してから進めること。

## 実装結果（2026-06-27 完了）

### 要検討の決定（ユーザー回答）

1. **フォークセッション名** → **固定名・上書き**（`claude-fork`。再 fork で kill+再作成、popup は単純トグル。fzf セレクタ不要）。
2. **skill か slash command か** → **slash command `/fork-scratch`**（`_claude/commands/` を新設し setup.sh で symlink 運用）。
3. **transcript flush の鮮度** → スモークテストで**フォークは起動ターンまでを含む**ことを確認（鮮度問題なし）。
4. **初回 fork 起動** → detached 起動 + resume を実機確認。なお初回 popup で claude の managed-settings 承認プロンプトが出る場合があり、ユーザーが Enter で進める（claude 標準挙動）。
5. **popup トグルキー** → **`prefix + b`**（旧 btw 枠を再利用）。

### 変更ファイル

- `_tmux.conf`: `bind b`（fork popup ビューア。`bind t` scratch の型をミラー、枠=緑系・🌿、閉じ側は detach+#4920 refresh、`-A` 不使用で2回押し回帰回避）。
- `scripts/tmux_fork_popup.sh`（新規）: popup の attach ロジック（has-session なら attach、無ければ案内。空セッション非生成）。
- `_claude/commands/fork-scratch.md`（新規）: フォーク投入 slash command（env guard + 内側クォート）。
- `setup.sh`: `~/.claude/commands` の symlink 対応 + migrate ループを全 per-file dir に整合（source 破壊経路を塞ぐ）。
- `scripts/tmux_fzf_jump.sh`: `claude-fork` を fzf jump 候補から除外（popup 専用セッション）。

### レビュー

cross-review（code-reviewer / architecture-reviewer / security-auditor + Codex 並行）実施。全5観点（quoting・2回押し回帰・script 堅牢性・env 展開・setup.sh 一貫性）PASS。指摘のうち env guard・内側クォート・「復帰」記述の訂正・migrate ループ整合・status-left 非対称の明文化を反映。status-left を global format に寄せる案は「fork の帯は静的（点滅不要）/ global format が最も壊れやすい行」を理由に **session 単位上書きを維持**（理由をコード内コメントに記録）。

### 残る手動確認

- 実機で `prefix + b`（C-t b）の popup **開閉の体感**（display-popup はインタラクティブで自動検証不可）。起動・resume・close ロジック・quoting は検証済み。
