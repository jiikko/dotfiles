# 070 research: 品質監査 (2026-08-20)

起票日: 2026-08-20

`/audit` 品質バッチ (broken-code / dead-code / resource-leaks / security / error-handling /
performance) の結果。forge Standard × 8 エージェント + 条件付き (security-auditor /
go-architecture-designer)。**統合フェーズは session limit で落ちたため、main agent が
生 findings を検閲して集約した**。

High 2 件は単独 issue に分離済み: [068](068-bug-snapshot-health-lock-owner-format-drift.md) /
[069](069-bug-deny-bare-tmux-kill-exemption-bypass.md) (どちらも実験で再現済み)。

以下は Medium。**main agent がコードで存在確認した項目には ✓ を付ける。それ以外は
未裏取りの候補**で、着手前にコード再確認が必要。

## 対応状況 (2026-08-21 時点)

この issue の項目は 2 セッションで分担して着手した。**下の各項目に `[済 <commit>]` が付いて
いるものは対応済み**。付いていないものは未着手 (または別セッションが進行中)。

- 本セッションが対応: 孤児サーバ回収の default socket 除外 / mtime の stat 方言差
- 別セッション (dotfiles-95) が対応: archive 完全性ガードと bypass 経路 / bin/ci-log の
  false green / URL の C1 漏れ / bin/tmux-toast 系 / terminal_profile の AppleScript ほか。
  裏取りの一次情報は `./tmp/audit-triage.md` (72 指摘の CONFIRMED / FALSE_POSITIVE 判定つき。
  tmp なので消えうる)
- **反証で却下された項目もある** (例: `_tmux.conf:551` の `#{socket_path}` 未 q: 化 /
  `bin/tmux-toast` の python3 fallback)。着手前にトリアージ表を確認すること

## 保存・復元まわり (resurrect)

- **archive 完全性ガードがコメントの主張より弱い** — `scripts/tmux_resurrect_save.sh:230`
  `tt_archive_ok` は `[ -s ] && gzip -t` だけで、同ファイルのコメントが宣言する
  「entry ≥ 1」を実装していない。entry 0 の tar.gz は非空かつ `gzip -t` を通るため、
  中身が空の archive を healthy と判定して直前世代の退避コピーを削除しうる。
  発火条件: 全 pane の capture-pane が失敗しても tar/gzip 自体は成功する状況
- **退行ガード bypass が archive 検証まで無効化する** — 同 `:333`。`tt_archive` の初期化が
  `TT_SAVE_ALLOW_REGRESSION != 1` ブロックの内側にあるため、正当な bypass 実行では
  archive 検証・修復・archive-broken ログがまるごと skip される。bypass を使う場面
  (状態が大きく動いた直後) は archive が壊れやすい局面と重なる
- **reject streak の escape hatch が退避 archive を消してから return する** — 同 `:408-413`
- **lock owner 同一性判定が 2 実装ある** — guards.sh (tab 区切り + lstart 生文字列) と
  `tmux_resurrect_save.sh:97-148`。068 の直接原因と同根
- ✓ **`ps -o lstart=` がロケール依存** — `scripts/lib/tmux_resurrect_guards.sh:63`。
  指紋文字列が LANG によって変わる (実測: ja_JP と C で別書式)。プロセス同一性判定が
  ロケール変更を跨ぐと不一致になる

## tmux サーバ状態

- **[済 61e9c49] 孤児サーバ回収が「生きている本番サーバ」も対象にしうる** —
  `scripts/tmux_reap_orphan_servers.sh:77`。孤児判定に canonical な default socket の
  除外がない。発火条件: 何かが default socket ファイルを削除し、かつ client が全 detach
  している瞬間に `tt` が reap を呼ぶ。スクリプト自身のコメントが「mktemp socket 上の
  孤児の衛生役」と役割を限定しているので、default socket 除外で目的は達成できる。
  **対応**: OS 既定の default socket (/tmp と /private/tmp の両表記) を保護リストに置いた。
  lsof のパス表記が環境依存 (macOS は /private/tmp を返す) で素の文字列比較では保護が
  黙って外れたため、先頭 /private を剥がして正規化している。テストは保護 + 陽性対照
  (保護を外すと同じプロセスが reap される) + 既定リストの中身を pin。変異 4 種で red 確認
- **watchdog の死因分類が貪欲マッチで別の pid を拾う** —
  `scripts/tmux_server_watchdog.sh:154` の `sed -n 's/.*[^a-z]pid=\([0-9]*\).*/\1/p'`
- **`#{socket_path}` が q: 展開されずに watchdog へ渡る** — `_tmux.conf:551`

## agent panel (既定 ON なので常時効く)

- **render ループの fork 過多** — `scripts/tmux_agent_panel.sh:312` 以降。2 秒 tick ごとに
  行数に比例した subshell (state_color / state_rank / rel_time が echo 返し) を出す。
  この repo は continuum の status interpolation を「5-10 fork/秒は基準に合わない」として
  捨てているので、同じ基準に対する内部不整合。対応は `zsh-hook-return-via-reply.md` と
  同思想 (変数代入で返す) + epoch を tick ごと 1 回に + ソートを単一 awk に畳む。
  まず `tests/tmux/bench_tmux.sh` に「1 tick のプロセス数」を足して予算化する
- **panel pane の同一性判定がスクリプト絶対パス完全一致** — 同 `:117` / `:254`。
  worktree 並行作業ではパスが違うため、別 worktree の panel を自分のものと認識しない /
  掃討できない
- ✓ **[済 7c064e6] `stat -f %m` が BSD 前提** — 同 `:205`。GNU stat では `-f` が別意味で算術エラーになり、
  hook 経由なので無音契約に反して pane にエラーが積まれる。同 repo の
  `tmux_snapshot_health.sh:49-53` は「GNU を先に試す」で既に回避済み (追随漏れ)。
  **対応**: guards.sh に `tt_mtime_of` (GNU-first) を集約し、snapshot_health のローカル定義も
  統合 (重複 2→1)。偽 GNU stat 環境で旧形が算術構文エラー・現行が正常を実測

## hook の無音契約

- **`bin/tmux-toast` が `set -euo pipefail` のまま tmux 呼び出しを保護していない** (:91,:100)。
  hook (after-split-window / pane-exited) から呼ばれるため、失敗時に pane へエラーが積まる
- **fallback 経路が python3 を無ガードで呼ぶ** — `bin/tmux-toast:147`。同ファイル :78-86 は
  width 算出で python3 不在を明示 fallback しているのに、ticks 算出だけガードがない。
  現行 tmux 3.7b では到達しない経路 (未確認リスク)
- **`_claude/hooks/tmux-mark-seen.sh:33-37` が pane 数ぶん `tmux if-shell` を fork する**
- **`_tmux.conf:475,478` の resurrect hook が共有観測ログへインラインで書いている** —
  同ファイル 428-431 のコメントが「インラインで書かないこと (default socket ゲートを通す)」
  と自ら禁じている形

## 外部入力の扱い

- ✓ **`_tmux.conf:346,353` が `git -C '#{pane_current_path}'` と素で埋めている** —
  パス名に `'` が含まれると引用符が閉じる。`scripts/CLAUDE.md` は同型の穴を
  `#{q:...}` で塞ぐ規約として明文化済みなので、ここだけ非対称。攻撃者が任意の
  ディレクトリ名を作れる前提が要るため severity は Medium
- **`scripts/terminal_profile_restore.sh:38-58` が .terminal の name を AppleScript の
  ソース文字列へ 5 箇所そのまま埋めている** — `"` を含む名前で AppleScript を脱出でき
  `do shell script` に到達する。スクリプト自身が任意 .terminal の受け入れを明示している。
  対応は osascript の `argv` 経由でデータとして渡す (エージェントが実測で確認済み)
- **`scripts/tmux_extract_popup.sh:78`** が抽出語からコマンド行を組み立てて send-keys する
- **`zshlib/_git_prompt.zsh:98`** が `.git/HEAD` 等由来の文字列を prompt へ素で埋める
- **`src/glogx/issues/body.go:115`** の URL 正規表現が C1 制御文字 (U+009B CSI 等) を
  終端に含めていない (sink は `url_picker.go:135`)

## glogx (Go)

- **`issues_watch.go:101`** の `watched` マップが印を降ろさないため、watch を失った
  ディレクトリが二度と Add され直さない
- **`issues_view.go:1719`** の幅の契約が producer と consumer で逆を向いており、実在の
  issue ファイルで枠が崩れる
- **`widthenv/widthenv.go:46`** の自作ガードが go test の結果キャッシュに素通りされ、
  意図した loud な失敗が出ない

## その他

- **`scripts/check_syntax.zsh:53`** の `_tmux.conf` 検査が終了コードだけを見ており、
  警告付きロードを pass にする
- **`scripts/test_changed.sh:142-143`** の写像が shell 系変更を一部のターゲットへしか
  写していない
- **`_zshrc:696-733`** の適用漏れ通知が結果ファイル名をシェル間で共有している

## 攻めて見つからなかった範囲 (次回の起点)

- 秘密情報のハードコード: repo 全体で検出なし (`.gitignore` と `_claude/settings.json` の
  分離が効いている)
- confirm 系スクリプトの fail-safe (`&&` 短絡): 変異実験で red になることを確認済み
  = 実装強制されている。一方 `--default=false` は未強制 (→ 071)
