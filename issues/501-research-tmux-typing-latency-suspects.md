# research: tmux 上のタイピングが時々もっさりする原因を層ごとに切り分ける

起票日: 2026-09-27
カテゴリ: research / performance
優先度: 高
対象: Terminal.app + tmux + zsh の「キー押下 → 文字が画面に出るまで」の体感遅延

## 背景

ユーザーから「tmux 上でタイピングがもっさりすることが多い。入力経路を軽量化できないか」という報告。

ここで扱うのは **Enter 後に prompt が戻るまでの遅さではなく、編集中の 1 キーごとの遅延**。
したがって `precmd` / `direnv` の 4〜5ms は原則この症状の主因ではない。
キー入力中に直接通る経路を優先して調べる。

概念上の経路:

```text
Terminal.app
  -> tmux client
  -> tmux server
  -> pane PTY
  -> foreground app
       - zsh ZLE
       - nvim
       - cat 等
  -> pane PTY
  -> tmux server 描画
  -> Terminal.app
```

**最適化を先にしない。**
まず「Terminal.app / tmux 本体 / この repo の tmux config / zsh ZLE plugin」のどこで差が出るかを A/B で確定する。

---

## 既知の事実

### 1. tmux の escape-time は既に短い

`_tmux.conf`:

```tmux
set -sg escape-time 5
```

通常文字の入力遅延をここから削れる余地は小さい。
`0` への変更を最初の対策にしない。

### 2. tmux status は fork ゼロ方針だが毎秒再評価

`_tmux.conf`:

```tmux
set -g status-interval 1
```

status 内の `#()` はほぼ排除済みだが、1 秒ごとに tmux server が複雑な format を展開する。

現在の status/window 表示には以下が含まれる:

- window fade
- `pane_current_command`
- `@last-touched`
- Claude 状態
- pro-con 状態
- scratch spinner / blink
- zoom
- schedkeys

「毎秒 1 回」なので平均負荷より、**タイピングと再描画がぶつかった瞬間だけ引っ掛かる**形を疑う。

### 3. pane border が常時 on で format が重い

`_tmux.conf`:

```tmux
set -g pane-border-status top
```

`pane-border-format` は少なくとも以下を評価する:

- `@schedkeys-at`
- `@claude_state`
- `pane_active`
- `pane_current_path`
- `pane_current_command`
- `window_zoomed_flag`

常時表示のため、**低遅延モード候補の最優先 A/B 対象**。

### 4. window 切替時の ignite は意図的に重い

`after-select-window[1]` / `client-session-changed` から
`scripts/tmux_ignite_current.sh` が起動する。

最大 8 フレーム、それぞれ tmux client fork + `refresh-client -S`。
`tests/tmux/bench_tmux.sh` / `bench_budgets.ci` でも、
refresh が tmux server の event loop で後続操作と直列化し、
window 切替時に数十 ms 相当のコストを持つことを既知としている。

**「window 切替直後だけ入力が重い」なら最有力。**

### 5. zsh-syntax-highlighting / autosuggestions はキー入力経路にいる

`_zshrc` は:

- zsh-autosuggestions
- zsh-syntax-highlighting

をロードしている。

issue 322 で autosuggestions の **precmd 側** 6.3ms は改善済みだが、
今回見るべきは precmd ではなく **ZLE の 1 キーごとの処理**。

特に長い command line、巨大 repo、補完状態、履歴 suggestion などで差が出る可能性がある。

---

# 容疑者

## S1: repo 固有 tmux 描画負荷

対象:

- `pane-border-status top`
- 長い `pane-border-format`
- `status-interval 1`
- window fade の format
- Claude/pro-con/schedkeys の表示

症状との整合:

- 常時入力中に発生しうる
- 「常に遅い」より「時々引っ掛かる」に合う
- CPU 計算というより tmux server event loop / redraw の競合なので ASM 化では解決しない

## S2: zsh-syntax-highlighting の ZLE 処理

症状との整合:

- 1 文字ごとに発生しうる
- tmux の中でしか気づいていないだけで、tmux の追加遅延と合算して閾値を超えている可能性
- command line が長い時だけ悪化するなら強い

## S3: zsh-autosuggestions の ZLE 処理

issue 322 で precmd の無駄な再bindは除去済み。
ただし suggestion 計算・描画自体は入力経路に残る。

特に履歴量・strategy・現在の入力文字列の長さとの相関を見る。

## S4: window ignite

常時タイピングではなく、以下のパターンなら最有力:

```text
window 切替
-> 0〜300ms の間にタイプ
-> 引っ掛かる
```

常時入力の主犯と混同しない。

## S5: Terminal.app + tmux の描画相性

bare tmux + bare foreground app でも重ければこちら。

例:

- Unicode / Nerd Font
- blinking cursor
- 大量 scrollback
- pane サイズ
- Terminal.app 側の再描画
- macOS 側の負荷

repo の config を直しても解消しない可能性がある。

## S6: tmux server 自体の混雑

バックグラウンドから大量に tmux client が接続している場合、
入力そのものではなく server event loop が詰まる。

関連:

- issue 500
- pro-con の tmux publish
- agent panel
- hook
- animation
- tests が大量に tmux client を作る時間帯

**入力遅延が CPU/プロセス負荷の高い時間帯だけ出るかも記録する。**

---

# A/B 検証

## Phase 1: まず「tmuxか zshか」を 5 分で分離する

同じ Terminal.app で比較する。

### A. tmux 外 + bare zsh

```sh
zsh -f
```

### B. 現在の tmux + bare zsh

```sh
zsh -f
```

### C. 現在の tmux + `cat`

```sh
cat
```

文字を打って、Enter 後にそのまま echo される経路を見る。
終了は `C-d`。

### D. 現在の tmux + 通常 zsh

普段の環境。

### 判定

| 結果 | 読み |
|---|---|
| A 軽い / B 重い / C 重い | tmux 側が主因 |
| B 軽い / D 重い | zsh ZLE/plugin が主因 |
| B 重い / C 軽い | zsh -f でも残る ZLE 自体との相互作用を疑う |
| A から重い | Terminal.app / macOS / system load 側 |
| 全部軽いのに普段だけ重い | 条件依存。window 切替直後、長い入力、特定 cwd、負荷との相関を取る |

**体感だけでなく、どの条件で「重い」が再現したかを issue に追記する。**

---

## Phase 2: bare tmux で repo config を完全に外す

既存 server を壊さない別 socket:

```sh
tmux -L latency-test -f /dev/null new
```

その中で:

```sh
zsh -f
```

さらに:

```sh
cat
```

### 判定

- bare tmux は軽い / 本番 tmux は重い
  -> **repo config 起因が確定**
- bare tmux でも重い
  -> tmux 本体 / Terminal.app / macOS / system load へ進む

終了:

```sh
tmux -L latency-test kill-server
```

---

## Phase 3: 本番 tmux config の容疑者を 1 個ずつ切る

🚨 一度に複数切らない。
どれが効いたか分からなくなる。

### 3-A. pane border

現在値を記録:

```sh
tmux show -gv pane-border-status
```

OFF:

```sh
tmux set -g pane-border-status off
```

数分タイピング。

戻す:

```sh
tmux set -g pane-border-status top
```

**第一候補。**

### 3-B. status periodic redraw

現在値:

```sh
tmux show -gv status-interval
```

停止:

```sh
tmux set -g status-interval 0
```

数分タイピング。

戻す:

```sh
tmux set -g status-interval 1
```

改善するなら次に 2 / 3 / 5 秒を比較する。

```sh
tmux set -g status-interval 5
```

※ これは検証。即 permanent にしない。
spinner / fade / status 更新仕様との trade-off を確認してから決める。

### 3-C. ignite

現在の hook を確認:

```sh
tmux show-hooks -g | grep -E 'after-select-window|client-session-changed'
```

一時停止:

```sh
tmux set-hook -gu 'after-select-window[1]'
tmux set-hook -gu client-session-changed
```

**window 切替直後の typing** を重点的に比較。

復元は手で hook を再構築せず:

```sh
tmux source-file ~/dotfiles/_tmux.conf
```

で戻す。

---

## Phase 4: zsh plugin を分離する

Phase 1 で `zsh -f` が軽く、通常 zsh が重い場合のみ進む。

### 最初に見る対象

1. zsh-syntax-highlighting
2. zsh-autosuggestions
3. 両方の組合せ

**source 行を消して本番設定を直接壊す前に、隔離した一時 rc / ZDOTDIR で比較する。**

最低限:

- plugin 無し
- syntax-highlighting のみ
- autosuggestions のみ
- 両方

で 1 キーの反応を比較する。

条件を最低 3 種類用意する:

1. 空 prompt で短い入力
2. 200〜500文字程度の長い command line
3. git repo 内

長い入力でだけ syntax-highlighting 有りが悪化するなら、
入力長に対する伸び率も測る。

---

# 計測を作る場合

体感確認を先に行い、差がある条件を見つけた後で自動計測を作る。

目的は absolute な「1 キー 3.2ms」の精密測定ではなく、
同一マシン・同一 Terminal/tmux 条件での **A/B 比較**。

候補:

- pty で tmux client を attach
- shell/app 側に marker を返させる
- 1文字送信から marker/echo 観測までを複数回取る
- median / p95 を記録
- 初回を捨てる
- wall clock ノイズが大きければ 100〜1000 キーの総時間で比較

🚨 `tmux send-keys` は root key table を通らず pane へ直接送る。
**人の実キー入力経路の代用として使わない。**
既存 `tests/tmux/test_ctrl_v_paste.sh` に同じ注意がある。

---

# ASM 化について

この issue では **ASM 化は候補から外す**。

理由:

- tmux / zsh 本体は既に native code
- 疑っているのは tmux server redraw / event loop / PTY / plugin hook
- 外部 ASM binary にすると fork/exec が増えて逆効果になりやすい
- CPU-bound な純計算 hotspot が計測で出た場合でも、まず C / zsh module / tmux upstream の改善を検討する

ASM を再検討する trigger:

1. Instruments/sample で CPU-bound な自作処理が hotspot として出る
2. fork/IPC/redraw を除外した pure computation が 1 キーごとに有意な ms を消費している
3. C 実装との比較で手書き ARM64 に実測上の意味がある

この 3 条件が揃ったときだけ。

---

# 期待する成果物

Claude は検証後、この issue に以下を追記する。

```text
## 実測 YYYY-MM-DD

環境:
- macOS:
- tmux:
- Terminal.app:
- pane 数:
- window 数:
- foreground app:
- CPU load:

A/B:
- tmux 外 + zsh -f:
- tmux 内 + zsh -f:
- tmux 内 + cat:
- tmux 内 + full zsh:
- bare tmux + zsh -f:

repo config:
- pane-border off:
- status-interval 0:
- ignite off:

zsh:
- syntax-highlighting off:
- autosuggestions off:

結論:
- 主因:
- 副因:
- 棄却した容疑者:
- permanent fix 候補:
```

「軽くなった気がする」だけで close しない。
**少なくとも主因 1 個を A/B で再現するか、全候補を棄却して次の観測点まで進むこと。**

---

# permanent fix の候補

結果が出るまでは実装しない。

### 案 A: Low Latency Mode

tmux option 1 個で以下をまとめて切替:

- pane-border-status off
- ignite off
- status-interval を 3〜5 秒
- fade / spinner の高頻度更新を抑える

コーディング中は low latency、必要時だけ rich UI。

### 案 B: event-driven UI へ寄せる

毎秒の status polling を減らし、
状態が変わった時だけ `refresh-client -S`。

ただし refresh の打ちすぎは ignite と同じ問題を再生産するので debounce 必須。

### 案 C: ZLE plugin の機能を絞る

zsh側が主因なら:

- highlighter を必要最小限へ
- autosuggestion strategy の見直し
- 長い BUFFER では一部処理を抑制
- plugin の置換

機能低下と latency の A/B を人が判断する。

---

# 受け入れ条件

- [ ] tmux 外 / 本番 tmux / bare tmux を同一 Terminal.app で比較した
- [ ] `zsh -f` と通常 zsh を比較した
- [ ] `cat` で zsh ZLE を通らない経路を比較した
- [ ] pane-border-status off を A/B した
- [ ] status-interval 0 を A/B した
- [ ] window切替直後の症状について ignite on/off を比較した
- [ ] zsh 側が主因なら syntax-highlighting / autosuggestions を個別に分離した
- [ ] 結論は「どの層で差が出たか」を実測/再現条件つきで残した
- [ ] permanent fix は原因確定後に別 issue またはこの issue の対応節で決めた
- [ ] ASM 化は CPU-bound hotspot の証拠がない限り行わない

## 関連

- `_tmux.conf`
- `tests/tmux/bench_tmux.sh`
- `tests/tmux/bench_budgets.ci`
- `scripts/tmux_ignite_current.sh`
- `_zshrc`
- `issues/done/322-perf-precmd-cost-is-dominated-by-third-party-hooks.md`
- `issues/338-human-zsh-precmd-verification-and-direnv-decision.md`
- `issues/500-bug-macos-kernel-zone-leak-from-tmux-clients.md`
