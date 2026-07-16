# tmux を「小さなツールの土台」として使う

## 概要

tmux はマルチプレクサであると同時に、外部依存ゼロで使える小さな UI 部品とコマンド／フォーマット／イベントフックの集合体でもある。`display-popup`（フローティング窓）・`display-menu`（選択メニュー）・`command-prompt`（入力）・`confirm-before`（確認）・`choose-tree`（ツリー選択）が「対話 UI の primitive」を、`new-session` / `send-keys` / `capture-pane` / `join-pane` / `set-hook` などが「自動化エンジン」を、`status-left` / `window-status-format` / `pane-border-format` の formats が「状態ダッシュボード」を提供する。

これらは bind / hook / シェルから起動でき、内部で任意の tmux コマンドや shell-command を「コマンド + 結果」の単位で実行する。fzf や gum のような外部 TUI を `display-popup` に載せれば、数十行のシェルで「曖昧検索 → tmux コマンド実行」のミニツールが組める。本リポジトリは実際に fzf ジャンプ／window 跨ぎ pane 移動／scratch popup／Claude 状態バッジ／resurrect の debounce 保存を、すべて自前 shell + 真の tmux 機能の組合せで実装している。

実機は **tmux 3.7b (macOS)**（本書の初版は 3.5a 時点で執筆）。本書では新しめの機能（`display-menu`、`display-popup` の `-x/-y`、`pane-scrollbars` 等）にはバージョン要件を明記する。`man tmux` で実在を確認できる機能だけを扱う。

---

## できること

### 1. フローティング UI と確認・選択 primitive

#### display-popup（フローティング窓）

【何ができるか】画面に浮かぶ矩形の中で任意の shell-command を実行する。fzf / gum / nested tmux などの TUI を載せる土台になる。

【使う tmux 機能】`display-popup`（alias `popup`）。`-E`=コマンド終了で閉じる、`-EE`=成功時のみ閉じる、`-B`=枠なし、`-w/-h`=大きさ（% 可）、`-x/-y`=位置、`-b`=枠線種別（single/rounded/double/heavy/simple/padded/none）、`-S`=枠スタイル、`-s`=中身スタイル、`-T`=タイトル、`-e`=環境変数注入。

【最小例】
```tmux
tmux display-popup -E -w 80% -h 60% -b rounded -T ' files ' 'nvim "$(fzf)"'
```

【この repo の実例】`/Users/koji/dotfiles/_tmux.conf` の `bind f/g/G` が `display-popup -E -w 85% -h 70%` で fzf スクリプトを起動。`bind t` は `-b heavy -S fg=colour33,bg=colour201 -s bg=colour17 -T` で枠を 2 色 + 中身を濃紺にした scratch popup。

【バージョン注意】`display-popup` は 3.2+。`-e`（環境変数）と `-x/-y` 位置指定・`#{popup_*}` 変数は 3.3 前後。3.5a で全て利用可。ただし **popup 内 shell-command では `#{...}` フォーマットも `-e` の値も展開されず、`TMUX_PANE` も無い**（3.6a で実測）。対象は popup 内シェルから `tmux display-message -p` で都度解決するのが定石。

#### confirm-before（確認ダイアログ）

【何ができるか】コマンド実行前に y/n を聞く。kill 系の誤爆防止の primitive。

【使う tmux 機能】`confirm-before`（alias `confirm`）`[-by] [-c confirm-key] [-p prompt] command`。`-y` で Enter=実行（危険操作では付けない）、`-b` で背景表示。

【最小例】
```tmux
bind X confirm-before -p 'kill pane? (y/n)' kill-pane
```

【この repo の実例】**意図的に使っていない**。確認はステータス行に出て見落としやすいため、kill 確認（`bind x/q`）・history 解放（`bind M-c`）は `display-popup` + `gum confirm` に置き換えている（`_tmux.conf` の `bind x` コメントに理由明記）。confirm-before の上位互換として popup を選んだ実例。

【バージョン注意】confirm-before 自体は古くから存在。`-b`（背景表示）は 3.2+。

#### display-menu（選択メニュー）

【何ができるか】項目名・ショートカットキー・実行コマンドの 3 つ組を並べた小さなメニュー。名前を `-` 始まりにすると disabled、空名で区切り線。fzf 無しで静的メニューを出す primitive。

【使う tmux 機能】`display-menu`（alias `menu`）`[-O] [-b border-lines] [-T title] [-x position] [-y position] name key command ...`。`-x/-y` は display-popup と同じ位置指定（C=中央 等）。

【最小例】
```tmux
bind m display-menu -T ' actions ' -x C -y C \
  'New window' n 'new-window' \
  'Split'      s 'split-window -v' \
  ''           '' '' \
  'Kill pane'  k 'kill-pane'
```

【この repo の実例】未使用（window ジャンプ・pane 移動は `choose-*` でなく fzf popup に寄せている）。導入余地のある未使用 primitive。

【バージョン注意】`display-menu` は 3.0+。`-b` 等のスタイル系は 3.4+ で拡充。3.5a で利用可。項目数が端末に収まらないと表示自体されない点に注意。

#### command-prompt（入力プロンプト）

【何ができるか】ステータス行で 1 行入力を受け取り、応答を `%%` / `%1..%9` に差し込んでコマンド実行する。rename や ssh 先入力などの primitive。

【使う tmux 機能】`command-prompt [-1bFikN] [-I inputs] [-p prompts] [-T prompt-type] [template]`。`%%`/`%1..%9` で応答展開、`-T` は command/search/target/window-target の補完、`-N`=数値のみ、`-i`=入力変化ごとに実行（インクリメンタル）。

【最小例】
```tmux
bind S command-prompt -p 'host:' "new-window -n %1 'ssh %1'"
```

【この repo の実例】直接 bind していない（rename は automatic-rename + pane_title 追従、検索は copy-mode の vi キーに寄せている）。man の例と同型の未使用 primitive。

【バージョン注意】古くからある。`-N`/`-k`/`-T` は 3.1+、`-b` は 3.3+。3.5a で全フラグ利用可。

#### choose-tree / choose-client / choose-buffer（ツリー・セレクタ）

【何ができるか】セッション/ウィンドウ/ペインを木構造で選ぶ。標準の `prefix+s/w` の正体。

【使う tmux 機能】`choose-tree [-GNrswZ] [-F format] [-f filter] [-O sort-order] [template]`。`-Z`=ズーム、`-s`=セッションのみ、`-w`=ウィンドウのみ。`choose-client` / `choose-buffer` も同系統。

【最小例】
```tmux
bind w choose-tree -Zw   # window をズーム表示の木から選ぶ
```

【この repo の実例】標準の choose-tree を fzf popup（`/Users/koji/dotfiles/scripts/tmux_fzf_jump.sh`、`bind f`）に**置換している**。fzf 版は「全セッションの window をアクティビティ順 + 相対時刻 + capture-pane プレビュー付き」で出し、choose-tree より情報量が多い。choose-tree は「ゴチャついて見づらい」として不採用（`_tmux.conf` の `bind g/G` コメント参照）。外部 fuzzy finder に寄せた設計実例。

【バージョン注意】`choose-tree` は 2.7+ で現形に。`-Z` は 2.7+、`-K` は 3.2+。3.5a で利用可。

---

### 2. 自動化エンジン（CLI / ソケット連携）

#### セッション一括構築

【何ができるか】外部スクリプトから `new-session -d` / `new-window` / `split-window` でレイアウト雛形を一発で作って attach する。

【使う tmux 機能】`new-session -d -s`、`new-window -t`、`split-window -h/-v -c`、`select-layout`、`attach-session`。

【最小例】
```bash
tmux new-session -d -s dev -c ~/proj
tmux split-window -h -t dev -c ~/proj
tmux select-layout -t dev tiled
tmux attach -t dev
```

【この repo の実例】`/Users/koji/dotfiles/zshlib/_tmux_session.zsh` の `_t_impl`（5 窓を `new-window` で量産して attach）。

#### 冪等な bootstrap と自動復元待ち

【何ができるか】`has-session` で存在判定して無ければ作る。OS 再起動直後は空セッションを先に作らず、自動復元の完了を待ってから attach する。

【使う tmux 機能】`has-session`、`show -gqv @option`（フラグ読取）、`attach-session \; display-message`。

【最小例】
```bash
tmux has-session -t "$name" 2>/dev/null && tmux attach -t "$name" || tmux new-session -s "$name"
```

【この repo の実例】`/Users/koji/dotfiles/zshlib/_tmux_session.zsh` の `_tt_impl` + `_tt_wait_for_restore`。サーバ未起動時に `__tt_hold_$$` という hold セッションを置いて総ペイン数=1 を保ち、resurrect の `restore_from_scratch` を有効化してスクロールバックごと復元させてから attach する。`@tt-restore-complete` フラグが立つまで待つことで部分復元 attach を防ぐ。

#### イベント駆動フック

【何ができるか】window/pane の構成変化や window 選択を契機に外部スクリプトをバックグラウンド起動する。

【使う tmux 機能】`set-hook -g window-linked / window-unlinked / after-split-window / after-kill-pane / pane-exited / after-select-window`、`run-shell -b`（非同期）、`#{window_id}` の format 引数渡し。

【最小例】
```tmux
set-hook -g after-split-window "run-shell -b '~/bin/on_split.sh'"
```

【この repo の実例】`/Users/koji/dotfiles/_tmux.conf` 末尾。`window-linked/unlinked/after-split-window/after-kill-pane/pane-exited` → `scripts/tmux_resurrect_debounced_save.sh`、`after-select-window` → `_claude/hooks/tmux-mark-seen.sh #{window_id}`。

【バージョン注意】これらの hook 名・`run-shell -b` は 3.5a で利用可。`run-shell` の format 引数展開は環境差があり、`tmux-mark-seen.sh` は `#{window_id}` が未展開で渡るケースを `display -p` でフォールバックしている。

#### debounce 保存パターン

【何ができるか】フックが連打されても「最後の 1 イベントから N 秒後に一度だけ」重い処理を走らせる。復元中・bootstrap 中はガードして last スナップショットを壊さない。

【使う tmux 機能】`run-shell -b` で hook から非同期起動、`show -gqv @flag` をフラグとして読取、`list-sessions -F '#{session_name}'` で状態判定。フラグ自体は `@` プレフィックスのユーザーオプションで自前管理。

【最小例】
```tmux
set-hook -g window-linked "run-shell -b '~/bin/debounced_save.sh'"  # スクリプト側で token + sleep + lock
```

【この repo の実例】`/Users/koji/dotfiles/scripts/tmux_resurrect_debounced_save.sh`（token を `mv` で atomic 更新 → sleep → 自分が最後なら `mkdir` lock を取って保存）。`@tt-restore-in-progress` は epoch を立て、降り損ねに備えて TTL 超過で無効化する。保存の直列化は `scripts/tmux_resurrect_save.sh` が単一 lock で担う。

#### pane 単位のユーザーオプションを状態ストアにする

【何ができるか】外部プロセスが書いた値（`@xxx`）を status-line / pane-border-format がリアルタイム表示する。「状態の正本=オプション、表示=format」を分離する。

【使う tmux 機能】`set -p -t $TMUX_PANE @claude_state '値'`（pane 単位 set）、`set -p -u`（unset）、formats `#{?@claude_state,...,}` / `#{m:*working*,...}` / `#{P:...}`（window 内全 pane 展開）、`#{window_active_clients}`（見えているか判定）。

【最小例】
```bash
tmux set -p -t "$TMUX_PANE" @claude_state '⚙ working'   # 表示は format 側が解釈
```

【この repo の実例】`/Users/koji/dotfiles/_claude/hooks/tmux-pane-state.sh`（working/input/idle/clear を `@claude_state` に書く + `window_active_clients=0` なら terminal-notifier 通知）と `_tmux.conf` の window-status-format / pane-border-format（`#{P:...}` で window 内全 pane のアイコンを並べる）。

#### 競合する状態更新を if-shell -F で atomic にする

【何ができるか】「現在値を読む → 条件付きで書く」をシェルで 2 段にすると隙間で別 hook の更新を踏むため、読取と条件付き書込を 1 コマンドにする。

【使う tmux 機能】`if-shell -F '#{==:#{@claude_state},🔔 input}' "set -p ..."`（format 条件で同期分岐、シェル fork なし）、`list-panes -F '#{pane_id}'`。

【最小例】
```tmux
tmux if-shell -F '#{==:#{@claude_state},🔔 input}' "set -p -t '$pid' @claude_state '🔕 seen'"
```

【この repo の実例】`/Users/koji/dotfiles/_claude/hooks/tmux-mark-seen.sh`（after-select-window 発火時、window 内各 pane の input だけを seen に降格。`if-shell -F` で read-modify-write の隙間を排除。codex 指摘を反映したと冒頭コメントに明記）。

#### capture-pane / pipe-pane（出力の吸い出し）

【何ができるか】ペイン出力をキャプチャしてプレビュー/ログ/grep に流す。`pipe-pane` なら出力を継続的にコマンドへ tee できる。

【使う tmux 機能】`capture-pane -p`（stdout へ）、`-e`（エスケープ保持=色付き）、`-S/-E`（行範囲）、`-J`（折返し結合）、`pipe-pane -o 'cmd'`（同一コマンドのトグル）。

【最小例】
```bash
tmux capture-pane -ep -t %3 | tail -40
# 継続パイプ: tmux pipe-pane -o 'cat >>~/out.#I-#P'
```

【この repo の実例】`/Users/koji/dotfiles/scripts/tmux_fzf_jump.sh` と `tmux_fzf_pane_move.sh` の fzf `--preview 'tmux capture-pane -ep -t {1} | tail -40'`。

【バージョン注意】`capture-pane -e` は 2.4+。3.5a で利用可。

#### send-keys（キー入力の注入）

【何ができるか】ペインへキー入力やコマンド文字列を注入して実行させる。複数ペインへ一括送信もできる。

【使う tmux 機能】`send-keys -t target 'cmd' Enter`、`-l`（literal）、copy-mode 用 `send-keys -X copy-pipe-and-cancel 'pbcopy'`。

【最小例】
```bash
tmux send-keys -t dev:1.0 'npm run dev' Enter
```

【この repo の実例】`/Users/koji/dotfiles/_tmux.conf` の copy-mode バインド `bind-key -T copy-mode-vi y send-keys -X copy-pipe-and-cancel "pbcopy"`。`bind M-c` は `list-panes -a -F '#{pane_id}' | xargs -n1 tmux clear-history -t` で全ペイン一括操作。

#### automatic-rename を OSC 2 と連動させる

【何ができるか】「アクティブペインで実行中のコマンド名」を window 名へ自動反映する。リネームはシェルの preexec/precmd が pane タイトル経由で駆動する。

【使う tmux 機能】`set -g automatic-rename on`、`set -g automatic-rename-format '#{pane_title}'`、`set -g allow-rename off`（`\033k` 直リネームを禁止し OSC 2 に一本化）。

【最小例】
```tmux
set -g automatic-rename-format '#{pane_title}'  # シェル側: printf '\033]2;%s\033\\' "$title"
```

【この repo の実例】`/Users/koji/dotfiles/zshlib/_tmux_window_name.zsh`（preexec で実行コマンド名を YAML マップ → OSC 2 で pane title セット、precmd で zsh に戻す。make/git は `_subcommands` whitelist で第 2 語も付ける）+ `_tmux.conf` の `automatic-rename-format '#{pane_title}'`。

#### #(shell-command) で format に外部出力を埋める

【何ができるか】format 内で外部スクリプトの出力を埋め込む。条件付き評価 `#{?...}` と組み合わせ、特定セッションでだけ動的に評価させて他では無コストにできる。

【使う tmux 機能】`#(shell-command)`（status-interval ごとに実行）、`#{?条件,真,偽}`（偽側の `#()` は評価されない）、`set -t session status-interval 1`（セッション単位の更新周期）。

【最小例】
```tmux
set -g status-left "#{?#{==:#{session_name},scratch},#(~/bin/blink.sh),静的}"
```

【この repo の実例】**現在は未使用**。scratch/prefix 点滅は旧実装で `#()` スクリプト（tmux_scratch_blink.sh、現在秒の偶奇返し）を毎秒評価していたが、毎秒 fork するため `#{T:@secfmt}`（strftime 展開）+ `#{e|m:...}`（剰余）の format 算術（fork ゼロ）へ置換して削除済み（`_tmux.conf` の status-left コメント参照）。

【バージョン注意】**`#()` 内に `$(...)` を直書きすると、tmux の `#()` パーサがその `)` を閉じ括弧と誤認して壊れる**（実測）。外部スクリプトに閉じ込めるのが定石。

#### if-shell でバージョン判定ガード

【何ができるか】新しめのオプションを古い tmux で `set` した時の警告を抑止する。`tmux -V` を sed で数値抽出して major/minor 整数比較する。

【使う tmux 機能】`if-shell 'シェル式' 'then-cmd' 'else-cmd'`、`tmux -V` のパース、`display-message`、`#{version}` フォーマット。

【最小例】
```tmux
if-shell 'v=$(tmux -V | sed "s/[^0-9.]//g"); maj=${v%%.*}; min=${v#*.}; min=${min%%.*}; [ "$maj" -gt 3 ] || { [ "$maj" -eq 3 ] && [ "$min" -ge 6 ]; }' \
  'set -g pane-scrollbars off' \
  'display-message "3.6+ 推奨"'
```

【この repo の実例】`/Users/koji/dotfiles/_tmux.conf` の `pane-scrollbars` ガード（3.6 で追加されたオプション。**3.5 以前で素に set すると "invalid option" 警告が出る**ため、if-shell で版数比較し 3.6+ のときだけ set、未満は upgrade を促す `display-message`）。本環境は 3.7b なので then 節（`pane-scrollbars off`）が発火する。

---

### 3. 状態ダッシュボード（formats による可視化）

#### window リストに状態アイコンを並べる

【何ができるか】各ウィンドウの Claude Code 作業状態アイコン（⚙=作業中 / 🔔=入力待ち / 🔕=既読 / ✓=完了）を別ウィンドウ分も含めて一覧する。`#{P:...}` で window 内全ペインを展開するので分割中はペイン分のアイコンが並ぶ（例 🔔✓）。

【使う tmux 機能】`window-status-format` / `window-status-current-format` に `#{?cond,A,B}` 条件分岐、`#{P:...}`、`#{m:pattern,str}`（glob マッチ）、`#[fg=colourN]` で色分け。

【最小例】
```tmux
setw -g window-status-format "#{?@my_state,#{?#{m:*busy*,#{@my_state}},#[fg=yellow]B,#[fg=green]I},} #I:#{=15:window_name} "
# 別プロセスが: tmux set -p -t %3 @my_state busy
```

【この repo の実例】`/Users/koji/dotfiles/_tmux.conf` の `window-status-format` / `window-status-current-format`。`@claude_state` を `#{P:...}` で展開し ⚙🔔🔕✓ を色分け。

【バージョン注意】`#{P:...}` と `#{m:}` は 3.x 系で有効。3.5a で動作確認済み。

#### pane-border-format（ペイン上端タイトルバー）

【何ができるか】各ペインの上端にペイン番号・カレントパス・実行コマンド・Claude 状態・zoom 解除ヒントを表示。アクティブペインは緑帯に黒文字で ACTIVE と出す。

【使う tmux 機能】`pane-border-status top`、`pane-border-format`、`#{pane_current_path}` / `#{pane_current_command}` / `#{pane_active}` / `#{window_zoomed_flag}`、`#{s|pat|rep|:str}`（置換でパスを ~ 短縮）。

【最小例】
```tmux
set -g pane-border-status top
set -g pane-border-format " #P: #{s|${HOME}|~|:pane_current_path} (#{pane_current_command})#{?pane_active,#[fg=black#,bg=green#,bold] ACTIVE ,} "
```

【この repo の実例】`/Users/koji/dotfiles/_tmux.conf` の `pane-border-status top` と `pane-border-format`（`@claude_state` 色分け + ACTIVE 緑帯 + 🔍 ZOOM ヒント）。

【バージョン注意】`pane-border-format` / `pane-border-status` は古くからある。`pane-border-indicators both`（端に矢印）は 3.4+、`pane-border-lines double/heavy` は 3.2+。

#### zoom 中のウィンドウを背景色で強調

【何ができるか】ペインを zoom（`prefix+z`）しているウィンドウのステータスセル背景を赤に反転。current 側は明るい赤、別ウィンドウへ移っても zoom が残っている非 current 側は暗い赤。

【使う tmux 機能】`window-status-format` 内で `#{?window_zoomed_flag,#[bg=colour160],..}` による背景反転。

【最小例】
```tmux
setw -g window-status-current-format "#{?window_zoomed_flag,#[bg=colour160#,fg=white],#[bg=blue]} #I:#{window_name} "
```

【この repo の実例】`/Users/koji/dotfiles/_tmux.conf` の `#{?window_zoomed_flag,...}` と pane-border-format 末尾の 🔍 ZOOM（colour160）。

【バージョン注意】`window_zoomed_flag` は 2.x から有効。版ガード不要。

#### アクティブ/非アクティブペインを面の背景色で識別

【何ができるか】非アクティブペインを暗いグレー背景に沈め、アクティブを濃紺背景にする。線 1 セルの色差より面全体の色差のほうが視野の端で瞬時に分かる。

【使う tmux 機能】`window-style`（非アクティブ背景）/ `window-active-style`（アクティブ背景）、`pane-active-border-style fg=bg`（境界線をベタ塗り化）、`cursor-style` / `cursor-colour`。

【最小例】
```tmux
set -g window-style        'fg=colour247,bg=colour234'
set -g window-active-style 'fg=terminal,bg=terminal'
set -g pane-active-border-style fg=colour46,bg=colour46
```

【この repo の実例】`/Users/koji/dotfiles/_tmux.conf` の window-style/window-active-style、pane-active-border-style、cursor-style blinking-block + cursor-colour。

【バージョン注意】window-style/window-active-style は古くからある。`cursor-style` / `cursor-colour` は 3.4+。nvim 等が自前で背景を塗るアプリ内では効かず、素のプロンプトのペインで最も効く。

#### 非表示ペインの状態遷移で macOS 通知

【何ができるか】画面で見えていないペインの状態が input(承認待ち)/idle(完了) に遷移したとき通知を出す。`window_active_clients=0` のときだけ通知。

【使う tmux 機能】hook 内から `tmux set -p -t $TMUX_PANE @claude_state`、`tmux display -p '#{window_active_clients}'` で可視判定、terminal-notifier の `-group` でペイン単位に上書き。

【最小例】
```bash
tmux set -p -t "$TMUX_PANE" @claude_state '🔔 input'
v=$(tmux display -p -t "$TMUX_PANE" '#{window_active_clients}')
[ "$v" = 0 ] && terminal-notifier -message '入力待ち' -group "tmux-$TMUX_PANE"
```

【この repo の実例】`/Users/koji/dotfiles/_claude/hooks/tmux-pane-state.sh` の `set_state()` と `notify_if_hidden()`。

【バージョン注意】`window_active_clients` は 3.0+。3.5a で動作確認済み。

#### scratch セッションだけソフト点滅

【何ができるか】特定セッションのステータスバーだけ毎秒マゼンタ↔赤で点滅させ強調する。SGR の blink 属性ではなく毎秒 bg 色を実際に切り替えるので端末非依存。

【使う tmux 機能】status-left に `#{?#{==:#{session_name},scratch},...}`、`@secfmt='%S'` を `#{T:@secfmt}` で strftime 展開、`#{e|m:秒,2}`（剰余）で 2 相の色切替、`set -g status-interval 1` で毎秒再描画。format 算術はサーバ内完結で fork ゼロ。

【最小例】
```tmux
set -g @secfmt '%S'
set -g status-left "#{?#{==:#{session_name},scratch},#{?#{e|m:#{T:@secfmt},2},#[bg=colour201],#[bg=colour196]} SCRATCH ,静的}"
```

【この repo の実例】`_tmux.conf` の status-left 点滅分岐（scratch 帯 + prefix 押下帯の 2 相点滅、スピナーは 4 剰余）。旧実装の `#()` スクリプト（毎秒 fork）は削除済み。

#### 分割ペイン + watch / tail -f の常時更新ダッシュボード

【何ができるか】ペイン自体を情報表示枠として使い、`watch` でメトリクス、`tail -f` でログ垂れ流しを見せる古典パターン。

【使う tmux 機能】`split-window -h/-v -c '#{pane_current_path}'`、`send-keys -t target 'cmd' Enter`、`capture-pane` で内容吸い出しも可能。

【最小例】
```bash
tmux split-window -v -c '#{pane_current_path}'
tmux send-keys -t :.+ 'watch -n2 docker ps' Enter
```

【この repo の実例】分割は `_tmux.conf` の `bind v/s/|/-`。`docs/tmux-plugins.md` のユースケース例（Window 3: logs で `tail -f`）が監視ペイン運用を示す。

#### status-interval による定期更新と continuum の前提

【何ができるか】status-interval で status の `#(shell)` や `#{...}` を定期再評価する。continuum の autosave も status 更新フックに乗るため `status on` + `interval > 0` + status-right の最小長確保が必要。

【使う tmux 機能】`set -g status-interval N`、`status on`、`status-right-length`。

【最小例】
```tmux
set -g status on
set -g status-interval 1   # この repo は prefix 点滅・放置フェードの駆動で 1 秒
set -g status-right-length 1
set -g status-right ""   # continuum autosave 用に最小長は残す
```

【この repo の実例】`/Users/koji/dotfiles/_tmux.conf` の status 設定と status-right-length 1。`docs/tmux-plugins.md`「status line 必須（autosave）」節に運用注意。値 0 や `status off` は continuum 由来 autosave を止める。

---

## このリポジトリの tmux ツール一覧（実例マップ）

| ツール / 設定 | 起動・契機 | 使う tmux 機能 | 実装ファイル |
| --- | --- | --- | --- |
| 全 window fzf ジャンプ | `prefix + f` | display-popup -E / list-windows -a / capture-pane プレビュー / switch-client | `scripts/tmux_fzf_jump.sh` |
| window 跨ぎ pane 移動 (get/give) | `prefix + g` / `G` | display-popup -E / join-pane / display -p で自 pane 固定 | `scripts/tmux_fzf_pane_move.sh` |
| scratch フローティング端末 | `prefix + t`（トグル） | display-popup -b heavy -S/-s/-T / has-session / detach-client / nested attach | `_tmux.conf` bind t |
| ペイン kill 確認 | `prefix + x`（現 pane）/ `q`（他全 pane） | display-popup + gum confirm / display-message -p で対象固定 / kill-pane | `_tmux.conf` bind x/q |
| history 解放確認 | `prefix + M-c` | display-popup + gum confirm / list-panes -a / clear-history | `_tmux.conf` bind M-c |
| resurrect debounce 保存 | window/pane 構成変化フック | set-hook / run-shell -b / @flag ガード / mkdir lock | `scripts/tmux_resurrect_debounced_save.sh` |
| 保存の直列化 wrapper | continuum/debounce/手動 C-s | 単一 lock / @resurrect-save-script-path 上書き | `scripts/tmux_resurrect_save.sh` |
| Claude 状態バッジ書込 | Claude Code hook | set -p @claude_state / window_active_clients で通知判定 | `_claude/hooks/tmux-pane-state.sh` |
| 🔔→🔕 既読降格 | after-select-window フック | if-shell -F で atomic な条件付き set | `_claude/hooks/tmux-mark-seen.sh` |
| 状態アイコン表示 | status / border の format | window-status-format / pane-border-format / #{P:...} | `_tmux.conf` |
| zoom 色強調 | format | #{?window_zoomed_flag,...} 背景反転 | `_tmux.conf` |
| アクティブ pane 面色 | 常時 | window-style / window-active-style / cursor-style (3.4+) | `_tmux.conf` |
| scratch / prefix 点滅 | scratch 表示時・prefix 押下時 | #{T:@secfmt} + #{e|m:} 剰余の format 算術（fork ゼロ） | `_tmux.conf` status-left |
| セッション bootstrap / 復元待ち | `t` / `tt` コマンド | new-session / has-session / @tt-restore-complete 待ち | `zshlib/_tmux_session.zsh` |
| window 名 = コマンド名追従 | preexec/precmd | automatic-rename-format '#{pane_title}' + OSC 2 | `zshlib/_tmux_window_name.zsh` |

---

## アイデア集（まだ作っていない小ツール案）

- **汎用コマンドランチャー（dmenu 風）**: `display-popup -E` + fzf で候補を出し、選択を `send-keys -t !` でアクティブ pane へ流し込む。既存の `bind f/g/G/x/q/t` と同じ土台で組める。fzf 無しなら `display-menu` で静的版も可。
- **popup 電卓 / メモ**: `display-popup -E -w 50% -h 40%` の中で `bc` や `$EDITOR ~/notes.md` を起動するだけ。`-E` で終了時に自動クローズ。
- **セッションランチャー**: `list-sessions -F` を fzf にかけて `switch-client -t`。プロジェクト一覧から飛ぶ。jump.sh の window 版を session 版にするだけ（参考: sesh / tmux-sm）。
- **git ブランチ fzf 切替**: popup 内で `git branch | fzf` → `git switch`。ブランチ名のプレビューに `git log --oneline` を出す。
- **ログ tail ダッシュボード**: `split-window` でログ枠を作り `send-keys 'tail -f ./tmp/app.log' Enter`。複数ログを `select-layout tiled` で並べる。
- **アクションメニューの統合**: 現在 kill / pane 移動などが個別 bind だが、`display-menu` で「pane を kill / 移動 / zoom / scratch を開く」を 1 メニューに集約する案（未使用 primitive の活用）。
- **宣言的セッションビルダー**: YAML/TOML でレイアウトと起動コマンドを書き 1 コマンドで立ち上げ（参考: tmuxinator / tmuxp）。本 repo は zsh 関数 `tt()` + resurrect に寄せているため未導入。
- **ペアプロ共有**: 同一ホストなら `tmux -S /tmp/pair new -s pair && chmod 777 /tmp/pair`、ネット越しなら tmate。本 repo は個人ローカル前提で未使用。

---

## バージョン注意

実機は **tmux 3.7b (macOS)**。本書の初版は 3.5a 時点で執筆したため、「3.5a で実測 / 動作確認済み」の記述はその時点のもの（3.7b でも後方互換で成立する）。バージョン要件自体は `man tmux` で確認したものを記載する。

- **display-popup**: 3.2+。`-e` / `-x/-y` / `#{popup_*}` は 3.3 前後。3.5a で利用可。
- **popup 内 shell-command の制約**: `#{...}` フォーマット・`-e` の値が展開されず `TMUX_PANE` も無い（3.6a で実測）。対象 pane は popup 内から `tmux display-message -p '#{pane_id}'` で都度解決する（`bind x/q/M-c` のコメント参照）。
- **popup overlay 再描画バグ（issue #4920）**: 3.5a〜3.6b で popup を閉じた後に枠が ~1 秒残るアーティファクトがあった。fix は 3.7 で収録。本 repo は閉じた直後に `refresh-client` で潰す回避（旧 `scripts/tmux_refresh_all_clients.sh`）を入れていたが、実機が 3.7b になったため回避は撤去済み。
- **`#()` パーサの落とし穴**: format の `#(...)` 内に `$(...)` を直書きすると `)` を閉じ括弧と誤認して壊れる（実測）。外部スクリプト化が定石。
- **display-menu**: 3.0+、スタイル系は 3.4+。`command-prompt -N/-T` は 3.1+、`-b` は 3.3+。`confirm-before -b` は 3.2+。
- **pane-scrollbars**: 3.6 で追加。3.5 以前で素に `set` すると "invalid option" 警告が出るため if-shell で版数ガードしている（本環境 3.7b では then 節 = off 設定が発火）。3.6a ではバー表示中にペインが 1 列 narrow され本文が reflow されるため off 運用。
- **pane-border-indicators**: 3.4+。`pane-border-lines double/heavy`: 3.2+。`cursor-style` / `cursor-colour`: 3.4+。`window_zoomed_flag` / `#{P:...}` / `#{m:}` / `window_active_clients`: 3.x（3.5a 動作確認済み）。
- **control mode（`tmux -CC`、iTerm2 統合）**: 3.5a の CONTROL MODE 節に実在。本 repo は Terminal.app 前提で未使用（`_tmux.conf` の mouse off コメントで将来の移行候補として言及）。

---

## 参考

- `man tmux`（実機の正式リファレンス。本書の機能・フラグはこれで確認）
- tmux 本体: <https://github.com/tmux/tmux>（issue #4920 = popup overlay 再描画）
- tmux-resurrect: <https://github.com/tmux-plugins/tmux-resurrect>
- tmux-continuum: <https://github.com/tmux-plugins/tmux-continuum>
- sesh（セッションランチャー）: <https://github.com/joshmedeski/sesh>
- tmuxinator（宣言的セッション）: <https://github.com/tmuxinator/tmuxinator>
- tmuxp（宣言的セッション）: <https://github.com/tmux-python/tmuxp>
- tmate（共有セッション）: <https://tmate.io/>
- iTerm2 tmux integration: <https://iterm2.com/documentation-tmux-integration.html>
- このリポジトリの永続化設計: `/Users/koji/dotfiles/docs/tmux-plugins.md`
