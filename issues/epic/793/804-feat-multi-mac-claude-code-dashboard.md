# 804 (feat): 複数の Mac で走る Claude Code を、1 台から見渡して操作する

起票日: 2026-10-16

> epic 793 の子。801 (ssh の副経路) のユースケース 1「人の番になった Mac を知らせる」を、1 つの機能として切り出した。

## 要望 (ユーザーの言 2026-10-16)

「僕は claudecode を複数の mac で同時に走らせているので、これが 1 つの mac から管理するような ux/ui があると便利だと思った」

## もうあるもの (dotfiles。1 台の中で閉じている)

- **状態を書く hook**: `_claude/hooks/tmux-pane-state.sh` が Claude Code の hook から呼ばれ、tmux のペインの option `@claude_state` に
  `⚙ working` / `🔔 input` (承認・質問の待ち) / `✓ idle` を、`@claude_state_since` に変わった時刻 (epoch) を書く。見えていないペインが
  input / idle になると macOS の通知も出す
- **1 台の中の一覧とジャンプ**: `scripts/tmux_agent_panel.sh` が全セッションを横断して `@claude_state` を持つペインを並べ、
  `scripts/tmux_agent_jump.sh` がそこへ飛ぶ
- **pro-con** (`src/pro-con`): PM と PG を分けて Claude Code を並列に回すカンバン。1 台の中の dispatcher が管理する
- 足りないのは「**Mac をまたいで**集めて、1 台から見て飛ぶ」こと

## 作り方の案

- **集める**: 各 Mac へ ssh で `tmux list-panes -a -F '#{@claude_state}\t#{@claude_state_since}\t#{session_name}:#{window_index}.#{pane_index}\t#{pane_current_path}\t#{pane_title}'`
  を打つ (読むだけ)。polling で始め、遅ければ hook がペインの状態と一緒に 1 行をファイルへ足し、ssh の `tail -F` で受ける (push。801 の作り方の案)
- **見せる**: 全 Mac の Claude のセッションを 1 枚に並べる。列は Mac・repo (ペインの cwd)・状態・その状態になってからの時間・ペインの題。
  🔔 input を一番上に、待っている時間の長い順に。数をメニューバーやアプリのアイコンに出す
- **飛ぶ**: 行を選ぶと、その Mac の画面へ切り替え (798 のショートカットと同じ仕組み)、ssh で `tmux switch-client` / `select-window` / `select-pane` を打って
  そのペインを前に出す。画面共有の向こうで、もう目の前に待っているペインがある状態にする
- **返す (後回し)**: 承認や短い返事を、画面を出さずに一覧から送る (ssh で `tmux send-keys`)。誤爆すると相手の作業を壊すので、input の状態のペインだけ・
  人が送る操作をしたときだけ・送る前に今の状態を読み直す、に限る。最初は作らない
- **相手の tmux を壊さない**: 打つのは読むコマンドと、人が選んだときの前に出すコマンドだけ。kill 系は打たない
  (`_claude/rules/tmux-probe-requires-socket-isolation.md` と同じ家族の事故を作らない)

## 先に安く試す道 (提案)

Swift のアプリを待たずに、**この仕組みだけを先に Go の TUI で試せる**。mac-lan-manager (747) は既に、Mac の一覧 (台帳 + Bonjour) と、
鍵だけの ssh で相手を読む口 (`src/mac-lan-manager/live/ssh.go` の `SSHRun`、`setup/remote.go` の `ObservePeer`) を持っている。
そこに「各 Mac の `@claude_state` を集めて並べる」1 画面を足せば、どの列・並べ方・通知が要るかを、使いながら決められる。決まった形を
Swift のアプリの一覧 (798 / モックの一覧 A・B) に持ち込む。どちらで先に試すかはユーザーの判断

## 確かめること

- [ ] ssh のセッションから、相手の tmux のサーバに届くか (tmux のソケットは相手のユーザーの `$TMUX_TMPDIR` か `/tmp/tmux-<uid>/default`。ssh の環境で同じ場所を見るか)
- [ ] 全 Mac が `tmux-pane-state.sh` の入った dotfiles を使っているか (使っていない Mac は状態が出ない)
- [ ] polling の間隔と負荷 (5 台 × 数秒ごとの ssh。接続を束ねれば軽いはず。801)
- [ ] tmux の外で走る Claude Code (Claude のデスクトップのアプリなど) をどう扱うか (tmux の option が無いので、この仕組みでは見えない)

## 受け入れ条件

- 1 台の Mac から、全 Mac の Claude Code のセッションの状態が一覧で見え、🔔 input になったセッションが数秒以内に一番上に出る
- 行を選ぶと、その Mac の画面に切り替わり、待っているペインが前に出ている

## 反証レビュー

省いた (2026-10-16)。dotfiles の既存の道具の記述は、`_claude/hooks/tmux-pane-state.sh` と `scripts/tmux_agent_panel.sh` の冒頭を読んで確かめた。
それ以外は未確認の案として書いた。
