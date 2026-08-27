# 125 human: prefix+m 予約入力ウィザードを実 tmux で確認する

起票日: 2026-08-27
期限: 2026-09-03
種別: human
関連: `scripts/tmux_schedule_keys.sh`（状態と tmux 副作用）/ `src/schedkeys/`（TUI）/
`tests/tmux/test_schedule_keys.sh`（配線）

自動テストは 2 層。Go 側 (`src/schedkeys`) が時刻の解釈・入力欄・カーソル位置を、シェル側の
stub テストが UI との契約・fire・取消・prune を固定している（いずれも変異で red 確認済み）。
**popup の見え方・IME の見え方・実サーバでの sleeper の生存**は tty と実サーバが要るため人が見る。

## 前提

まず conf を reload する（`prefix+R` → 確認 → 実行、または `tmux source-file ~/.tmux.conf`）。
reload しないと `bind m` は乗らず、撤去済み launcher (prefix+Enter) も残ったまま。

## 確認してほしいこと

1. **launcher が消えたか**: reload 後に `prefix+Enter`（= `prefix+C-m`）を押すと「Launcher」
   メニューではなく **予約入力ウィザードが開く**こと（同じ席に `bind Enter` を置いて上書き。
   source-file は既存 bind を消さないので conf から行を消しただけでは残っていた）
2. **ウィザードの表示**: `prefix+m` で popup（cyan 枠「予約入力」）が開き、「新規予約 /
   予約一覧・取消 (N 件)」の 2 択が出ること。UI は gum ではなく自作の TUI
   （`src/schedkeys`、bubbletea v2）に変わった。72x16 の枠に収まっているか

3. **1 画面フォームで通しを見る**: 新規予約 → 1 画面に「いつ（候補の並び）」「文字列」
   「→ HH:MM に送る (5m)」が出ること。
   - 既定フォーカスは文字列欄。そのまま `echo hello` と打ち、Enter で予約できること
   - Tab で「いつ」の行へ移り、左右キーで候補（5分後〜8時間後 / 時刻 / 自由）を選べること。
     選ぶたびに下の「→ HH:MM に送る」が変わること
   - 「時刻」を選ぶと HH:MM の欄が現れ、`09:00` のように過ぎた時刻を入れると翌日になること
     （プレビューが 20 時間超になる）
   - 「自由」で `1h30m` / `90` / `1:30` のいずれかを入れると 1h30m になること
   - 不正な入力（`25:00` など）で Enter を押してもフォームに留まり、理由が出ること
   - **5 分後で実際に予約し、5 分後にその pane で `echo hello` が実行されること**（`-l`
     リテラル + Enter）。送信時に右下 toast が出るか（装飾なので出なくても不具合ではない）

3b. **日本語入力中の未確定文字が入力位置に出るか**（2026-08-27 報告の修正確認 / 今回の作り直しの主目的）:
   文字列欄で IME をオンにして日本語を打つ。未確定（下線）の文字が入力中のカーソル位置に出ること。
   確定 → backspace で 1 文字ずつ消えること、`echo こんにちは` で予約して実際にその文字列が
   打ち込まれることも見てほしい。ずれるようなら報告してほしい（bubbletea v2 の
   `tea.View.Cursor` で本物のカーソルを置いている。ここが効いていないことになる）

3c. **一覧と取消**: 30 分後くらいの予約を入れてから `prefix+m` → 予約一覧。残り / 送り先 /
   文字列が桁揃えで並ぶこと（日本語の window 名でも崩れないこと）。選んで Enter → gum の確認
   （既定 No）→「取消する」で消えること。Esc で戻ったとき何も消えないこと

4. **別 window に移っていても元の pane に届く**: 予約後に他の window へ移動して待つ。
   送り先は予約時の pane（pane_id 固定）であり、今見ている pane には送られないこと
5. **`Enter` という文字列が化けないか**: 文字列に `echo Enter C-c` を入れて予約し、
   その通りの文字列が打ち込まれること（キー名として解釈されて改行や中断にならない）
6. ~~reload 越しに予約が生きるか~~ → **隔離 -L サーバで実測済み（2026-08-27）**: run-shell -b の
   sleeper は `source-file` を跨いで生存し、`kill-server` で死ぬ。reload で予約は消えないので
   人の確認は不要。実装のヘッダコメントに残した

## 崩れていたら

- 見た目の問題（枠のサイズ・文言の切れ）は `_tmux.conf` の `bind m` の `-w/-h` と
  `scripts/tmux_schedule_keys.sh` の gum header 文言で調整する
- 送信されない / 別 pane に送られる は不具合。`~/.cache/tt-schedule-keys.log` に
  new / fire / cancel / prune の行が残るので、それを添えて issue にする
