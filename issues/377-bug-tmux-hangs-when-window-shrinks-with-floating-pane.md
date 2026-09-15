# floating pane が残っていると、window の縮小**または**小さい client の attach で tmux サーバが CPU 100% でハングする

種別: bug / priority: high (操作不能になる。復旧は `kill -9` のみ)
起票: 2026-09-15 / 対象: tmux 3.7b (Homebrew, 2026-07-04 install)

## 症状

**引き金は 2 つあり、どちらか一方で起きる**:

1. 画面サイズ変更・AirPlay・外部ディスプレイ抜き差しで client が縮む
2. **リサイズせず、小さいターミナルから attach するだけ** (下の 🚨 節。こちらは当初見落としていた)

いずれの場合も tmux サーバが
**CPU 100% で無応答**になる。`tmux ls` / `display-message` / `send-keys` すら返らない。
`kill-server` も届かないので復旧は `kill -9` しかない。

## 前提: floating pane は clamp されず、window も client まで縮まない

tmux 3.7b は縮小時に floating pane (`new-pane -X/-Y`) を移動も縮小もせず、作成時の座標のまま残す。
さらに **window 自身も client 幅まで縮まない**。実測 9 ケースすべてで

```
縮小後の window 幅 = max(client 幅, 元の window 幅 - (floating pane の幅 - 1))
```

(例: 200 桁で幅 40 の pane → client 80 でも window は 161 桁のまま。client はその一部しか映さない)

この「window が縮みきらない」だけでも実害がある (画面の右側が見えない) が、本題は下記のハング。

## 発火条件 (実測。基準は window ではなく **client**)

**`pane_left` が client 幅を数セル超える** かつ **`pane_top` が client 高さより小さい** と発火する。

- 横: client 幅と同じ (+0) / +1 では発火せず、+5 / +8 / +10 / +60 / +140 はすべて発火 (閾値は +2〜+5 の間。未特定)
- 縦: `pane_top` が client 高さ以上なら発火しない (#14 / #16)。1 行だけ内側なら発火する (#24)
- この規則で**実測 12 行すべてが説明できる**。ハングのスタックにある `server_client_ensure_ranges` が
  **client ごと**の関数であることとも整合する

🚨 **初版と 2 版目に書いた「新しい window 幅を超える」は誤り**だった。基準を window にすると
[50] (X81 OK) と [51] (X90 HANG) を区別できない (縮小後 window 幅はどちらも 161 で、pane はどちらも内側)。
**window 幅は client の要求値まで縮まない**: 実測 12 行すべてで

```
縮小後の window 幅 = 元の window 幅 - (floating pane の幅 - 1)   (client 幅を下回る場合は client 幅)
```

(例: 200 桁で幅 40 の pane → client 80 でも window は 161 桁のまま。client はその一部しか映さない)

## 🚨 リサイズは必要条件ではない — 小さい client を attach するだけで踏む

`window-size` は既定 `latest` (この repo は未設定)。**大きい画面で生まれた floating pane が残ったまま、
小さいターミナルから attach するだけでハングする** (実測: 200x50 の window に X156 の pane を作り、
SIGWINCH を一度も撃たずに 80x24 の client を attach → CPU 98.7% で無応答)。

本番は client が 3〜4 本 (370 / 324 / 257 桁) 同時 attach される構成なので、
**「AirPlay / 画面サイズ変更」は引き金の 1 つにすぎず、「小さいターミナルを開いて attach」でも同じ**。

### 実測マトリクス

ログ: `tmp/377-verify.log` (gitignore。結論は本 issue が正本)。
各ケースで ①`new-pane` の rc ②縮小**後**の window / pane ジオメトリ ③毎秒の redraw 強制
(`send-keys` で `CSI 2J`) を記録している。判定は `display-message` の round-trip
(`timeout 3` + Python 側 `timeout 6` の二重待ち。パイプ待ちで固まらないようにする)。

| # | floating pane (X,Y / 幅x高) | client | pane_left − client 幅 | pane_top vs client 高 | 縮小後 window | 結果 |
|---|---|---|---|---|---|---|
| 22 | X60,Y0 40x3 | 80x24 | −20 | 内 | 161x23 | OK |
| 32 | X80,Y0 40x3 | 80x24 | +0 | 内 | 161x23 | OK |
| 50 | X81,Y0 40x3 | 80x24 | +1 | 内 | 161x23 | OK |
| 80 | X85,Y0 40x3 | 80x24 | +5 | 内 | — | **HANG** |
| 81 | X88,Y0 40x3 | 80x24 | +8 | 内 | — | **HANG** |
| 51 | X90,Y0 40x3 | 80x24 | +10 | 内 | 161x23 | **HANG** (CPU 100%) |
| 14 | X160,Y46 44x10 | 80x24 | +80 | **外** | 157x23 | OK |
| 16 | X99,Y46 100x3 | 80x24 | +19 | **外** | 101x23 | OK |
| 24 | X160,Y47 40x3 | 100x48 | +60 | 内 (47 < 48) | 161x47 | **HANG** |
| 90 | X160,Y0 40x3 | 100x48 | +60 | 内 | 161x47 | **HANG** (#24 の縦だけ変えた対照) |
| 25 / 42 | X220,Y0 150x10 (panel 相当 / **実物の `render`**) | 80x24 | +140 | 内 | 221x23 | **HANG** 両方 |
| 26 | X220,Y0 150x10 | 257x50 | −37 | 内 | — | OK |
| 63 | X0,Y0 150x10 (左寄せ panel。client から 70 セルはみ出す) | 80x24 | −80 | 内 | 221x23 | OK |
| 0 | なし | 80x24 | — | — | — | OK |
| — | X156,Y0 40x3 / **リサイズせず 80x24 の client を attach** | 80x24 | +76 | 内 | — | **HANG** (CPU 98.7%) |

- **pane の出力量は無関係** (`sleep` だけの pane でも発火。#11)
- **redraw を毎秒強制しても OK 側は OK のまま** (trigger 8/8 成功、CPU 0%)。「何も起きなかっただけ」ではない
- `display-popup` (glogx / fzf / 確認ダイアログ) は **安全**。popup 起動を canary (マーカーファイル) で
  確認し、縮小の実効も確認したうえで 3 条件すべて OK

ハーネス: `pty.fork` で attach client を作り `TIOCSWINSZ` で SIGWINCH を送る (実 AirPlay と同じ経路)。
隔離サーバ (`-L vrf*` / `-f /dev/null`) で実施、本番 socket は触っていない。
**限界**: #42 以外は `sleep` の手製 pane で、本番の conf (resurrect / continuum / hook) は読ませていない。
マトリクスが確定させたのは tmux 側の挙動であって、この repo のスクリプトの挙動ではない。

## 原因 (ハング中の `sample` から)

```
window_pane_read_callback → input_parse → input_csi_dispatch
  → screen_write_clearendofscreen → screen_write_collect_flush
  → screen_redraw_get_visible_ranges → server_client_ensure_ranges → xrecallocarray
    → recallocarray → memmove (2030) / bzero (279) / free_medium→madvise (247)
```

- pane が **CSI J (ED) を書いたとき**に通る経路。redraw が trigger
- **「確保が発散する」わけではない**: `Physical footprint 34.3M / peak 41.4M` で頭打ちで、内訳は
  medium ゾーンの malloc→全体 memmove→free の往復。**有界だが大きい配列を再確保し続けている**
- 「終わらない」の根拠は sample 1 本ではなく、**起動から約 2 分 52 秒後も同じ状態**だったことと、
  復旧が `kill -9` しかなかったこと
- このスタックは floating pane **2 枚** (X160,Y46 40x3 + X156,Y0 44x10) の構成で採った。
  マトリクスの各行 (1 枚) とは別構成である点に注意

## upstream

- [PR #5582 "Keep floating panes inside the window when it shrinks"](https://github.com/tmux/tmux/pull/5582)
  が **2026-09-11 に OpenBSD へ適用済み** (GitHub 側へは後日)。clamp が入れば発火条件自体が消える
- **3.7c の CHANGES にこの修正は入っていない** (`raw.githubusercontent.com/tmux/tmux/3.7c/CHANGES` で確認。
  jemalloc / scrollbar / message-format / unzoom-before-floating の 5 件のみ)。`brew upgrade tmux` では直らない
- PR にハング (CPU 100%) の記述はない。**ジオメトリ問題としてのみ報告されている**

## この repo で floating pane を作るもの

| 機構 | 位置 | 寿命 | 危険度 |
|---|---|---|---|
| `scripts/tmux_agent_panel.sh` (`C-t a`) | X = `win_w - 150`, Y=0 | 常駐 | **高**。ON の間ずっと条件を満たす (#42 で実物を実測) |
| `bin/tmux-toast` | X = `win_w - box_w`, Y = `win_h - 3` | **2〜8 秒** | 中。#24 で発火を実測。既定 2 秒だが `-d 8` が 2 本 (`tmux_verify_restore.sh:63` / `tmux_server_watchdog.sh:108`)、`-d 4` が 1 本 (`tmux_schedule_keys.sh:618`)。**無人で出るのはこの 3 本**で、特に verify_restore は**サーバ起動 + 自動復元の直後** (= ディスプレイ構成が変わった後に attach し直す局面) に出る。`tmux_paste_clipboard.sh` は `-F` でデバウンスを外すので同時に 2 枚生きうる |
| `prefix + *` (tmux 3.7 の既定バインド。この repo は unbind していない) | 既定 X=4, Y=2 / 100x12 | 手動 | 低。左上なので単独では安全側。ただし floating pane はマウスで移動できるので右へ寄せると同条件 |

🚨 **panel の「OFF」は揮発する。構成上の既定は ON。** `_tmux.conf:433` に `set -g @agent_panel_on 1` が
あり、conf 自身が「reload はこの set で表示に戻る (off の選好は reload / サーバ再起動を跨がない。仕様)」と
明記している。`cmd_toggle` の OFF は `set-option -gu` = 実行時のみ。再 ON の経路:

1. **`prefix R`** — `tmux_reload_confirm.sh:31-33` はサーバ起動から 60 秒を過ぎていれば**確認なしで即 reload**
2. **サーバ再起動 / 再 boot** — conf が読まれた時点で ON
3. ON になった後の pane 作成は自動 (`client-attached` / `after-select-window[2]` / `client-session-changed[1]`
   → `follow` → `create_panel`)。**新しいターミナルで attach しただけで生える**

2026-09-15 時点の本番サーバは実行時に OFF で floating pane は 0 件だが、これは
**次に reload するか再起動するまでの状態**にすぎない。

## 対応案 (未着手。判断待ち)

1. **✅ 採用・実施済み (2026-09-15)**: `_tmux.conf` の `set -g @agent_panel_on 1` を削除し、既定を非表示にした。
   panel の常駐露出が構造的に消える。座標・テスト・他の表示 (window-status の ⚙🔔🔕✓ / pane-border /
   `C-t A` のジャンプ) は無変更。副次的に、window 切替ごとの kill+create と、それが誘発していた
   toast 通知・resurrect の debounce 保存 (~20MB/回) も止まる。
   ただし**緩和であって修正ではない** (`C-t a` で ON にした瞬間に元のリスクへ戻る)
2. **右寄せをやめて左寄せにする** (X=0 側)。#63 で実測済み (client 80 に対し 70 セルはみ出すが無事)。
   根拠は観測行だけでなく不変条件でもある (`pane_left = 0` は常に client 幅未満)。副作用は見た目だけではない:
   toast は最下段なので**プロンプトの入力行に被り**、panel は左上なので**本文の左端を隠す**。
   さらに `tests/tmux/test_agent_panel.sh:102` (`-X 50`) と `tests/tmux/test_tmux_toast.sh:155`
   (`-X 192 -Y 47` を行ごと固定) の期待値を同じ commit で書き換える必要がある
3. **toast を floating pane 経路から外し、既存の tty 直描画 fallback (`bin/tmux-toast:150` 以降) を常用する**。
   toast 由来の floating pane が 0 になる。副作用は点滅の復活 (コード自身が「原理的に残る」と明記) と、
   `client_tty` 1 本にしか出ないこと
4. upstream の clamp が入った版へ上げる。**3.7c では直らない** (`brew install --HEAD tmux` は可能だが、
   PR #5582 が HEAD に入っているかは未確認。HEAD は未リリースの回帰も引く)
5. 現状維持 (panel を ON にしない運用 + toast の露出を受容)。ただし上記のとおり OFF は揮発する
6. upstream へハングとして報告する (PR #5582 のスレッドに実測を出す)

🚨 どの案を採る場合も: **座標計算が `bin/tmux-toast` と `scripts/tmux_agent_panel.sh` に別実装で 2 つある**。
片方だけ直すと「片方だけ左寄せ」になる。

## 却下した対策案 (再提案を防ぐ記録)

- **「作成時に window 幅へ clamp する」** → 無効。ハングは**作成後**の縮小 / attach で起きるので、
  生成時に収めても座標は据え置かれる。clamp するなら基準は `#{window_width}` ではなく
  **全 attach client の最小幅**だが、それでも縮小時のハングは塞げない
- **「`client-resized` hook で panel を作り直す」** → 危険。ハングは同じサーバの event loop 内で起きるため、
  hook の `run-shell` がキューに入っても処理されない可能性がある (未確認。間に合わない方に倒れると
  「対策が入っているのに同じハング」という新しい失敗モードになる)

## 敵対レビューで潰した論点 (再提出を防ぐ記録)

- **「`new-pane` の rc を捨てているので OK 行は pane が作られていなかったのでは」** → 却下。
  再実測で全ケース `rc=0`、`list-panes` にも floating pane が出ている
- **「OK 行は redraw が来なかっただけでは」** → 却下。毎秒 `CSI 2J` を強制しても OK 側は 8/8 で無事
- **「ケース間で残骸が汚染しているのでは」** → 却下。ケースごとに別 socket・別サーバで、
  サーバ pid を名指しで `kill -9` して socket を消している。実行後の残骸は 0 件
- **「drain スレッドが詰まって偽ハングでは」** → 却下。HANG 側も `drain生存=True`、CPU 99〜100%
- **「発火条件が実測に合っていない」** → **採用**。基準を window 幅から client 幅へ全面的に書き直した
- **「resurrect / continuum が `@agent_panel_on` や floating pane を保存・復元するのでは」** → 却下。
  `vendor/tmux-plugins/tmux-resurrect/scripts/save.sh` はユーザーオプション (`@...`) を保存しない。
  再 ON の経路は conf だけ。ただし保存済みスナップショット 104 個 (2026-09-07〜09-15) に floating の
  layout 節が 0 件だったのは「panel が OFF だった 8 日間の上界」であり、**保存中に toast が生きていれば
  layout に焼き付き、`render_panes()` に映らない = toggle でも消せない pane が復元されうる** (未確認)
- **「Homebrew がパッチを当てているのでは」** → 却下。`INSTALL_RECEIPT.json` は `poured_from_bottle:true` /
  `used_options:[]` / `spec:"stable"`、formula の `patches` は空。入っているのは素の upstream 3.7b

## 進捗

- [x] 対応案の選択 → **案 1 (既定を非表示にする)** をユーザー判断で採用 (2026-09-15)
- [x] 実施: `_tmux.conf` の `set -g @agent_panel_on 1` を削除し、理由と「変更可能になる条件」を
      同じ場所にコメントで残した。`scripts/tmux_agent_panel.sh` の「デフォルト表示」を前提にした
      コメントも同じ commit で直した。`make test` は EXIT=0 / 失敗 0 件 (agent_panel のテストも緑)
- [ ] **反映は未完**: 動いているサーバには `prefix R` (reload) か次回のサーバ起動まで効かない。
      実行時の値は既に OFF なので、reload しても ON には戻らない

## 敵対レビューからの follow-up (2026-09-15 に対応済み)

- **「tmux の pane 生成は rc を見る」** → **production に対象なし**。`bin/tmux-toast` は
  `|| exit 0`、`scripts/tmux_agent_panel.sh` は `|| return 1` で元から rc を見ている
  (`display-popup` の 2 箇所は `exec` なので rc は親へ透過)。rc を捨てていたのは本 issue の
  実測ハーネスだけで、それは下の残タスクに記録済み
- **「floating pane の座標計算が 2 箇所に別実装」** → 解消した。共通化の過程で**実バグを 1 件発見**:
  tmux は「幅 == window 幅」「高さ == window 高さ」を受理しない (rc=1) のに、panel 側だけが
  境界を許しており、**幅 150 以下の window では panel が一度も出ていなかった** (高さ側は clamp 自体が無かった)。
  計算を `scripts/lib/tmux_float_geometry.sh` (`tt_float_geom`) へ寄せ、単体テスト 14 ケースと
  変異検証 2 本 (旧実装の復元 / 高さ clamp 削除) を付けた

## 残タスク
- [ ] **実測ハーネス側の穴**: `verify*.py` の `case()` は後始末で `kill -9 <server pid>` を撃つだけで
      **死んだことを確認していない**。実際にケース 24 のハングしたサーバが生き残り、**23 分間 CPU 100% で
      回り続けていた** (手で `kill -9` して回収。2026-09-15)。再実行する人は同じ残骸を作るので、
      teardown を「kill → `ps -p` で不在を確認 → socket 削除」に直すこと
      (`verify-execution-not-just-exit-code.md`「判定は成果物で」の teardown 版)
- [ ] 横の閾値 (+2〜+5 の間) は未特定。左寄せ / 既定 OFF を採るなら不要
- [ ] **未検証**: ①縦だけ縮む場合 (`pane_left` は client 内 / `pane_top` が client 高を超える) は未実測
      ②左寄せ構成で拡大⇄縮小を 10 往復させたときの累積耐性 ③`brew install --HEAD tmux` に PR #5582 が
      入っているか ④保存済み layout に toast の floating が焼き付く経路
