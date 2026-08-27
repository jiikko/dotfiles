# 125 human: prefix+m 予約入力ウィザードを実 tmux で確認する

起票日: 2026-08-27
期限: 2026-09-03
種別: human
関連: commit 683831c（実装）/ `scripts/tmux_schedule_keys.sh` / `tests/tmux/test_schedule_keys.sh`

自動テストは stub 方式（偽 tmux / gum）で判定ロジック・fail-safe・send-keys の形を固定し、
変異 6 本で red を確認済み。**popup の見え方と、実サーバでの run-shell -b sleeper の生存**は
tty と実サーバが要るため人の目で確認する。

## 前提

まず conf を reload する（`prefix+R` → 確認 → 実行、または `tmux source-file ~/.tmux.conf`）。
reload しないと `bind m` は乗らず、撤去済み launcher (prefix+Enter) も残ったまま。

## 確認してほしいこと

1. **launcher が消えたか**: reload 後に `prefix+Enter`（= `prefix+C-m`）を押すと「🚀 Launcher」
   メニューではなく **予約入力ウィザードが開く**こと（同じ席に `bind Enter` を置いて上書き。
   source-file は既存 bind を消さないので conf から行を消しただけでは残っていた）
2. **ウィザードの表示**: `prefix+m` で popup（cyan 枠「⏰ 予約入力」）が開き、「新規予約 /
   予約一覧・取消 (N 件)」の 2 択が出ること。72x14 の枠に header・入力・確認文（2 行）が
   収まっていて、切れていないこと
3. **短い予約で通しを見る**: 新規予約 → 「いつ送る？」は **プリセット (5 分後〜8 時間後) の選択**。
   末尾に「時刻を指定… (HH:MM)」「自由入力… (90 / 1h30m / 1:30)」の逃げ道がある。
   まず「5 分後」を選ぶ → 文字列 `echo hello` → 確認「予約する」。status に
   「予約: 5m後に <session:index name> へ送る」が出て、**5 分後にその pane で `echo hello` が
   実行される**こと（`-l` リテラル + Enter）。送信時に右下 toast が出るか（toast は装飾なので
   出なくても不具合ではないが、出ないなら報告してほしい）
   - 「時刻を指定」で `HH:MM` を入れると確認文に「(HH:MM) に送る」と出て、過ぎた時刻なら翌日扱い
     （確認文の「N h後」が 20 時間超になる）ことも一度見てほしい
   - 「自由入力」で `1h30m` / `90` / `1:30` のどれかを入れ、確認文が「1h30m後」になること
   - popup は 72x16。10 行のプリセット一覧 + ヘッダが枠内に収まっているか
3b. **日本語入力中の未確定文字が入力位置に出るか**（2026-08-27 報告の修正確認）: 文字列入力で IME を
   オンにして日本語を打つ。未確定 (下線) の文字が `> ` の直後、カーソル位置に出ること
   （修正前は gum input の偽カーソルのせいで 2 行下に出ていた）。確定 → backspace で 1 文字ずつ消える
   こと、`echo こんにちは` で予約して実際にその文字列が打ち込まれることも見てほしい。
   なお文字列入力だけ gum でなく readline (`read -e`) なので、見た目が他の入力と少し違う
   （placeholder なし・矢印で編集可）。違和感が強ければ報告してほしい

4. **別 window に移っていても元の pane に届く**: 予約後に他の window へ移動して待つ。
   送り先は予約時の pane（pane_id 固定）であり、今見ている pane には送られないこと
5. **一覧と取消**: 30 分くらいの予約を 1 件入れてから `prefix+m` → 予約一覧。
   「残り / 送り先 / 文字列」の行が出て、選ぶ → 確認「取消する」で消えること。
   Esc で戻ったとき何も消えないこと
6. **`Enter` という文字列が化けないか**: 文字列に `echo Enter C-c` を入れて予約し、
   その通りの文字列が打ち込まれること（キー名として解釈されて改行や中断にならない）
7. ~~reload 越しに予約が生きるか~~ → **隔離 -L サーバで実測済み（2026-08-27）**: run-shell -b の
   sleeper は `source-file` を跨いで生存し、`kill-server` で死ぬ。reload で予約は消えないので
   人の確認は不要。実装のヘッダコメントに残した

## 崩れていたら

- 見た目の問題（枠のサイズ・文言の切れ）は `_tmux.conf` の `bind m` の `-w/-h` と
  `scripts/tmux_schedule_keys.sh` の gum header 文言で調整する
- 送信されない / 別 pane に送られる は不具合。`~/.cache/tt-schedule-keys.log` に
  new / fire / cancel / prune の行が残るので、それを添えて issue にする
