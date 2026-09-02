# 166 feat: 入力予約がある pane に「HH:MM に入力予約あり」を常時表示する

起票日: 2026-09-02
関連: `scripts/tmux_schedule_keys.sh` (予約の状態と tmux 副作用) / `src/schedkeys/` (ウィザード TUI) /
`_tmux.conf` の `bind m` (prefix+m) / issue 125 (ウィザードの human 確認、未読)

## 要望

prefix+m の入力予約 (N 時間 M 分後にこの pane へ文字列を送る) を仕掛けた pane では、
**その pane の左下あたりに「HH:MM に入力予約あり」が出ていてほしい**。

今は予約の存在を知る手段が「prefix+m → 予約一覧」を開くことしか無く、予約したことを忘れた
pane に別の作業を打ち込んでいて、発火時刻に予約文字列が混入する形の事故を防げない。
「この pane には時限の副作用が仕掛かっている」が見えているのが本題で、時刻はその次。

## 現状 (実測 2026-09-02)

- 予約 1 件 = `$STATE_DIR/<id>.job` (行順に pane_id / 発火 epoch / 文字列 / socket_path / サーバ pid)。
  発火は `tmux_schedule_keys.sh fire <id>` を `run-shell -b` で sleeper として起こす。
  取消・stale 掃除 (`prune_stale`) もこのスクリプトが持つ (`scripts/tmux_schedule_keys.sh` 冒頭コメント)
- pane 単位の常時表示は無い。予約成立・発火・取消は toast (`bin/tmux-toast`) と `display-message` で
  一過性に知らせるだけ
- `_tmux.conf` に `pane-border-status` / `pane-border-format` の設定は無い (= tmux 既定の off)。
  status line は `status-interval 1` で「再描画は format 展開のみで fork ゼロ」を方針にしている (`_tmux.conf:72-73`)
- tmux は 3.7b

## 設計案

### 状態の持ち方: pane スコープのユーザーオプションに写す

予約の正本は `.job` ファイルのまま、**表示用に pane オプション `@schedkeys-at` (値 = "HH:MM") を持つ**。

- `new_reservation` で `tmux set-option -p -t "$pane" @schedkeys-at "HH:MM"`
- 発火 (`fire` の送信後)・取消 (`cancel_job`)・stale 掃除 (`prune_stale`) で `set-option -pu`
- pane が消えればオプションも消えるので、pane 側の後始末は要らない
  (sleeper の設計と同じ「pane と一緒に消える」整合)
- 同じ pane に複数予約があるときは**最も早い発火時刻**を出し、その件数を添える ("14:30 ほか 1 件")。
  値の再計算は `.job` を走査して行う (`jobs_tsv` が既にその走査を持つ) ので、
  新規 / 取消 / 発火のたびに「この pane の残り予約から再計算して set / unset」の 1 関数に集約する

format 側から `#()` で `.job` を読む案は採らない。`status-interval 1` の毎秒再描画で fork が走り、
「再描画は format 展開のみ」の方針 (`_tmux.conf:72-73`) を破る。

### 表示位置 (サンプルで見てから決める。`decide-layout-in-sample-renderer-first.md`)

| 案 | 実現 | 利点 | 難点 |
|---|---|---|---|
| A. pane 枠の下辺 | `pane-border-status bottom` + `pane-border-format "#{?@schedkeys-at,#[bg=colour226#,fg=colour16] ⏰ #{@schedkeys-at} に入力予約 #[default],}"` | 「その pane の左下」に一番近い。pane ごとに独立 | border-status を on にすると**全 pane** に 1 行の枠が常時付く (予約が無い pane も 1 行減る)。tmux は pane 単位で border-status を on/off できないので、予約ゼロのときは window 単位で off に戻す等の切替が要る |
| B. status-right の島 | active pane の `@schedkeys-at` を status-right に表示 | 枠を増やさない。status-left の島 (`_tmux.conf:99`) と同じ作法 | 「今見ている pane」の分しか出ない。予約した pane を離れると見えない (要望の「その pane の左下」ではない) |
| C. A + B | 予約がある window だけ A、常時 B | 両方の穴を埋める | 切替の状態管理が増える |

推し: **A を、予約がある window だけ on にする形**。`set-option -w -t <window> pane-border-status bottom` を
`@schedkeys-at` の set / unset と同じ関数で切り替える (window 内の残り予約が 0 になったら `off`)。
ただし border 1 行分が消えたり出たりするので、まず `./tmp` のサンプル (隔離 `-L` サーバ) で
「1 pane の window」「分割済み window」「予約の無い隣 pane」の 3 形を見て決める。

表示文字列の規律: 全角と半角を同じ列に縦に並べない (`no-mixed-width-columns-in-terminal-ui.md`)、
幅が揺れる記号 (⚠️) は使わない、`#` は `##` にエスケープ (`msg_escape` と同じ)。

## 不変条件 (テストで固定するもの)

1. `.job` が存在する pane には表示があり、存在しない pane には無い。**新規 / 取消 / 発火 / stale 掃除 /
   サーバ再起動後の prune** の全経路で一致する (どれか 1 経路で unset を忘れると幽霊表示が残る。
   `survey-receiver-guards-before-passing-new-values.md` の要領で、状態を書き換える箇所を先に列挙する)
2. 表示は format 展開だけで更新される (毎秒 fork しない)。`tests/tmux/` の stub 方式で
   `set-option -p ... @schedkeys-at` の呼び出しを記録して固定する
3. 表示の有無で予約の動作 (送信・取消) が変わらない。表示側の失敗 (set-option 失敗) は無音で、
   予約自体は成立させる (`fire` の無音契約と同じ。scripts/CLAUDE.md)
4. 予約が複数ある pane では最早の時刻が出る

## 未確定 (実装前に決める)

- 「左下」を pane 枠の下辺で満たすか (案 A)、status line で妥協するか (案 B)。サンプルを見て決める
- 発火が近い (残り 1 分等) ときに色を変えるか。まずは固定色で出し、要望が出たら足す
- issue 125 (ウィザードの human 確認) が未読のまま。表示を足すと確認項目が増えるので、
  125 を先に消化するか、125 に本件の確認項目を追記するかを決める

## テスト観点

- `tests/tmux/test_schedule_keys.sh` の stub テストに「new で set-option -p が呼ばれる / cancel・fire・prune で -pu が呼ばれる」を足し、
  1 経路ずつ変異 (unset の削除) で red を見る (`mutation-verify-new-tests.md`)
- 見え方は human issue へ (実 pty が要る)。隔離 `-L` サーバで先に自分で見る
  (`tmux-probe-requires-socket-isolation.md`「human に回す前に隔離サーバで測れないか問う」)

## レビュー状態

起票時の反証レビューは未実施 (本文の事実は起票者が `scripts/tmux_schedule_keys.sh` / `_tmux.conf` / `tmux -V` で直接確認したが、設計案は反証されていない提案として扱う。着手時に設計レビューを通す)。
