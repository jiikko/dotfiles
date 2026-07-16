# Claude Code 風「暗めオレンジ基調」テーマへの移行設計 (terminal / tmux / nvim)

作成日: 2026-07-16
目的: 現在の配色 (ニュートラル暗灰の地 + ショッキングピンク現在地 + シアン fade) を、
Claude Code の UI のような「暖かい暗色の地 + 落ち着いたオレンジのアクセント」基調へ揃える。
前提資産: 色の定数化と変更ガイドラインは整備済み ([docs/theme-colors.md](../docs/theme-colors.md)、
nvim は palette.lua / tmux は @cur-accent 等)。**このおかげで変更箇所は「定数」単位で済む**。

---

## 参照色 (本設計で採用する近似値)

Claude / Anthropic 系としてよく知られる色 (公式パレットの出典 URL は無し。アプリの見た目
からの作者採用値として扱う) と、256色 (cterm) の最良近似。近似の選定方法: xterm256 の
16〜255 の実 RGB に対する sRGB 単純ユークリッド距離の最小値 (python で機械計算。
`v(x)= x==0 ? 0 : 55+40x` の cube 式 + grayscale 8+10n を対象、2026-07-16 実施):

| 名前 | hex | cterm 近似 | 距離 | 用途候補 |
|---|---|---|---|---|
| Coral (Claude ブランドの橙) | `#D97757` | **173** (#d7875f) | 18 | 現在地アクセント |
| Book Cloth | `#CC785C` | 173 | 19 | (Coral と同枠。173 が両方の近似) |
| Kraft | `#D4A27F` | **180** (#d7af87) | 16 | 選択テキスト (Visual) |
| Manilla | `#EBDBBC` | **223** (#ffd7af) | 24 | 明るい文字色。⚠️ gruvbox light1 (#ebdbb2) とほぼ同色 = 既存 palette と自然に調和する |
| Claude 暗背景 | `#1F1E1D` | **234** (#1c1c1c) | **4** | 地色 |
| Claude 暗背景2 | `#262624` | **235** (#262626) | **2** | バー地 |

**最重要の発見**: Claude の暗背景 2 色は既存の地色 234/235 と距離 4/2 でほぼ同一。
つまり「暖かさ」は 256 色の分解能以下であり、**cterm 側の地色は変更不要**。
基調の warm 感は下記の 3 層構造のうち Terminal.app プロファイル (フル RGB が使える唯一の層)
だけが担える。

## 大前提と制約

- **主環境は 256 色** (SUPPORT_TRUECOLOR=false。docs/theme-colors.md の制約節参照)。
  tmux/nvim の色はパレット 256 色に量子化される
- **この repo の主環境 (256色 + Terminal.app) では、Terminal.app のプロファイル
  (背景色/文字色/カーソル色) がパレット外のフル RGB を持てる唯一の層**。tmux の `bg=default`・
  nvim の `ctermbg=NONE` はこのプロファイル色を透過する (truecolor 対応端末なら tmux/nvim の
  gui 色でもフル RGB を扱えるが、主環境では使えないという条件付きの話)
- hex が設計の真・cterm はその忠実な近似 (既存規律)。新色は必ず {hex, cterm} の組で定義

## 設計方針: 3 層で基調を作る

```
層1: Terminal.app プロファイル … 暖かい地 (#1F1E1D) と明るい字 (#EBDBBC) の「基調」を RGB で持つ
層2: tmux / nvim の地色      … 可能な所は default/NONE で層1 を透過。明示が必要な所は 234/235 (近似≒同色)
層3: アクセント定数          … 現在地=Coral 173 / 選択=Kraft 180 / 通知=橙 208 (既存) / 危険=赤 (既存)
```

## ロール別 新旧対応表

### Phase 1 (推奨・低リスク: 基調の骨格)

| ロール | 現行 | 新 | 変更箇所 |
|---|---|---|---|
| ターミナル地/字/カーソル | (既定 or 手動設定) | 地 `#1F1E1D` / 字 `#EBDBBC` / カーソル `#D97757` | Terminal.app プロファイル (下記) |
| **現在地** (tmux 島 + nvim 選択タブ) | ショッキングピンク #ff00af/199 | **Coral `#D97757`/173** (黒字 bold は維持) | `@cur-accent` + `palette.accent.current_pink` (対で。ガイドライン記載のペア) |
| アクティブ pane の地 (素シェル) | 紺 colour17 | `bg=terminal` (層1 の warm 地を透過。「アクティブ=暖色そのまま / 非アクティブ=無彩 234 で冷ます」の対比に反転)。⚠️ **Phase 1 の対象は window-active-style のみ**。colour17 は他に message-command-style / copy-mode-match-style / scratch popup 内側 (`tmux_scratch_popup.sh -s bg=colour17`。_tmux.conf コメントが「active pane と同色で統一」と明記する意図的設計) でも使われており、それらは Phase 1 では「システム紺」として残す (下記 Phase 2 参照)。`bg=terminal` の受理は tmux 3.7b 実機で確認済み (set→show 読み戻し OK。透過の見た目はプロファイル適用後に目視) | `window-active-style` + 周辺コメント (_tmux.conf の window-style 説明 401-407 行付近は「アクティブ=濃紺」前提の記述なので同時更新) |
| 選択テキスト (Visual) | ローズ #d3869b/175 | **Kraft `#D4A27F`/180** (暖色系で基調に馴染ませる) | `palette` に kraft 追加 + Visual 参照差し替え |

- 現在地の色名は実態に合わせ **`current_pink` → `current_accent` に rename** する。
  ⚠️ rename は key 変更なので参照は自動追従しない (値だけ変えるなら palette 1 箇所で済むが、
  名前が pink のまま嘘になる)。追従対象の全サイト (2026-07-16 時点の grep 実測):
  `palette.lua` (定義 + accent コメント) / `_nviminit.lua` (Visual 分離コメント + bufferline
  selected 4 entry) / `_tmux.conf` (@cur-accent のペアコメント) / `docs/theme-colors.md` (4 参照)。
  **検証条件: 適用後に `rg -n current_pink` が 0 件**
- nvim の colorscheme は gruvbox/retrobox のまま (元々 warm 系で、Manilla≒light1 の一致もあり
  基調と自然に調和する。フル自作 colorscheme は scope 外)

### Phase 2 (任意・ライブで試して判断)

| ロール | 現行 | 案 | 備考 |
|---|---|---|---|
| 最近作業 fade | シアン ramp 51→23 | **採用 (ユーザー決定 2026-07-16): バイオレット ramp 201→164→127→90→53** (cube の r=b 対角。算術 `16+37L` = 既存シアン式の係数 7→37 の差し替えのみで基数 16 は共通。`@fade-hot-bg` も同式で自動導出)。メタファーは**黄昏の残光**: 橙の陽 (いまここ) が沈んだ場所に紫の残光が残り、闇 (地) へ溶けていく | fg 閾値の再調整が必要 (90/53 の暗い 2 段は黒字が読めない → 明灰の適用範囲を広げる)。⚠️ hot 201 は現行の「システム magenta 家族」と同色 — 下の衝突解消とセットで適用する |
| **通知 (bell)** | 橙 208 | **採用 (ユーザー決定): シアン 51** (セル反転 = bg51×黒字)。fade から解放されたシアンを「稀なイベントの ping」へ転用する — 常在させると浮いた寒色が、たまにしか出ない通知なら最強の目立ち役になる | `window-status-format` の bell 分岐 / `window-status-bell-style` の 2 箇所。copy-mode-mark-style (208) は別役割なので据え置き可 |
| システム magenta 201 家族 (衝突解消) | message-style / mode-style / copy-mode-current-match / blink / scratch チップ・枠 | **推奨: メッセージ系をシアン家族へ** — message-style → bg51×黒 (alert-bell 帯も message-style 経由なので bell セルと自動で揃う ✓) / copy-mode-current-match → bg51×黒 / mode-style (copy-mode 選択) → bg45 等のシアン系。「シアン=システム/通知」に意味を集約し、fade hot 201 との衝突を根から消す | blink (PREFIX/SCRATCH) と scratch のチップ/popup 枠の 201 は「scratch の視覚アイデンティティ (ピンク)」なのでライブ判断: 紫 fade と紛れるなら 213 (#ff87ff 明ピンク) へずらす。全 201 サイト: status-left blink×3 / status-format[1] チップ / message-style / mode-style / current-match / scratch popup 枠 (grep: colour201) |
| 〃 (不採用に降格) | — | ゴールド ramp 220→178→136→94→52 (`10+42L`) / アンバー (bell 衝突) / イエロー (尾が olive) | バイオレット採用により代替案へ。ゴールドは bell をシアンへ移した後なら衝突なしで復活可能な第2候補 |
| 一時メッセージ/点滅 | マゼンタ 201 | 維持 (システム色として対比を残す)。徹底するなら 166 (#d75f00) | message-style / status-left の blink 3 箇所 (@blink-phase 参照側) / copy-mode current match |
| システム紺 (colour17 家族) | message-command-style 地 / copy-mode-match 地 / scratch popup 内側 | 維持 (シアン字との組が確立したシステム配色)。徹底するなら 234/235 へ寄せる | `message-command-style` / `copy-mode-match-style` / `scripts/tmux_scratch_popup.sh` の `-s bg=colour17` (⚠️ scratch は「active pane の紺と同色統一」の意図コメントがあり、Phase 1 で active 紺を廃止すると統一根拠が消える→コメント更新 or 同時に変える。docs/tmux-as-platform.md の記述例 2 箇所も追従) |
| アクティブ pane 枠 | 緑 46 | 維持 (「稼働中」の意味色。warm 地に補色で立つ)。徹底するなら Coral 173 | pane-active-border-style / ACTIVE 帯 |
| tmux バー地 | 235 | 維持 (≒#262624)。徹底するなら `status-style bg=default` で層1 透過 | prefix 点滅の bg 反転との干渉をライブ確認 |
| nvim 地 (256色) | retrobox 234 | 維持 (≒#1F1E1D)。徹底するなら `hl.set("Normal", { ctermbg = "NONE" })` で層1 透過 | 透過は scrollview/vimade 等の bg 前提を崩しうるのでライブ確認必須 |

## 変更箇所一覧 (定数単位)

| # | ファイル | 箇所 | Phase |
|---|---|---|---|
| 1 | Terminal.app | プロファイル新設 (地 #1F1E1D / 字 #EBDBBC / カーソル #D97757)。設定→プロファイル→書き出しで `mac/ClaudeWarm.terminal` として repo に保存 (リストア手段) | 1 |
| 2 | `_tmux.conf` | `@cur-accent 'colour199'` → `'colour173'` | 1 |
| 3 | `nvim/lua/dotfiles/palette.lua` | `accent.current_pink` → `current_accent = { hex="#D97757", cterm=173 }` (rename + 値変更)。`kraft = { hex="#D4A27F", cterm=180 }` を追加 | 1 |
| 4 | `_nviminit.lua` | bufferline 4 entry の参照名追従 (rename 分のみ)。Visual を kraft 参照へ | 1 |
| 5 | `_tmux.conf` | `window-active-style 'fg=terminal,bg=colour17'` → `'fg=terminal,bg=terminal'` (active 紺廃止) + 401-407 行付近の「アクティブ=濃紺」前提コメントと scratch 統一根拠 (403 行付近) の更新 | 1 |
| 6 | `docs/theme-colors.md` | 色マップ表を新値へ更新 (触ったら直す)。`docs/tmux-as-platform.md` の window-active-style 記述例 (2 箇所) も追従 | 1 |
| 7 | `_tmux.conf` | fade をバイオレットへ: `@fade-ramp-color` と `@fade-hot-bg` の係数 7→37 を**対で**差し替え (基数 16 共通)。fg 閾値の再調整 (90/53 の暗い 2 段は黒字が読めない → 明灰の適用範囲を max-2 以上へ)。docs/tmux-window-fade.md の「シアン採用の経緯」も supersede 追記 | 2 |
| 7b | `_tmux.conf` | bell をシアンへ: `window-status-format` の bell 分岐 (bg208→51) + `window-status-bell-style` | 2 |
| 7c | `_tmux.conf` | システム magenta の衝突解消: `message-style`・`copy-mode-current-match-style` → bg51×黒 / `mode-style` → シアン系。blink×3 と scratch チップ/枠 (status-format[1]・`tmux_scratch_popup.sh`) はライブ判断 (維持 or 213) | 2 |
| 8 | `_tmux.conf` / `_nviminit.lua` / `scripts/tmux_scratch_popup.sh` | (Phase 2 徹底時のみ) message-style / システム紺家族 (message-command・copy-mode-match・scratch 内側) / 枠 / バー地 / Normal 透過 | 2 |

## 適用手順と検証 (ガイドラインの手順に従う)

1. **Terminal.app プロファイル**を作って切り替える (これだけで基調の 8 割が変わる。
   SUPPORT_TRUECOLOR=false は変更しない)
2. tmux: `tmux set -g @cur-accent colour173` をライブで打って島の見えを確認 →
   `_tmux.conf` へ書き戻し → `tests/tmux/test_tmux.sh`
3. nvim: palette を変更 → highlight A/B dump (ガイドラインのコマンド) で差分が
   意図した group のみか確認 → `tests/nvim/test_nvim.sh`
4. 目視: bufferline 選択タブ / tmux 島 / Visual / fade が期待の色か。
   コントラスト懸念 (Coral 173 地 × 黒字、Kraft 180 地 × シンタックス色) をここで判定
5. docs/theme-colors.md を同じ変更で更新

ロールバック: tmux/nvim 側は定数を旧値へ戻すだけ (旧値はこの表と git 履歴にある)。
**Terminal.app だけは repo 外の状態**なので、適用前に現在の既定プロファイル名を控えておき、
戻すときは 設定→プロファイル でそのプロファイルを既定に戻す (新プロファイルは削除不要)。

## 適用後の色サンプル (最終パレット)

Phase 1 適用後に画面に存在する色の一覧:

| ロール | hex (設計の真) | cterm | 見える場所 |
|---|---|---|---|
| 地 (基調) | `#1F1E1D` | 234 (透過含む) | ターミナル地・pane・nvim 背景・bufferline fill |
| バー地 | `#262624`≒ | 235 | tmux ステータスバー |
| 基本文字 | `#EBDBBC` (Manilla≒gruvbox light1) | 223/250/245 | 通常テキスト・タブ文字 (明→暗の 3 段) |
| **現在地** | `#D97757` (Coral) | **173** | tmux current 島・nvim 選択タブ (黒字 bold) |
| 選択テキスト | `#D4A27F` (Kraft) | **180** | nvim Visual |
| 最近作業 (fade) | バイオレット `#ff00ff`→`#5f005f` | 201→164→127→90→53 | tmux window list (黄昏の残光。Phase 2 で移行) |
| 通知 (bell + メッセージ帯) | シアン `#00ffff` | 51 | bell セル反転・message-style (alert 帯/: プロンプト)・copy-mode current match |
| マーカー (未保存/選択バー) | 橙 `#ff8700` | 208 | bufferline indicator・incline ● (nvim 側。bell と役割分離) |
| 点滅/scratch アイデンティティ | マゼンタ `#ff00ff` 系 | 201 (紫 fade と紛れるなら 213) | prefix/SCRATCH 点滅・scratch チップ/枠 (ライブ判断) |
| 危険 | 赤 | 160 (zoom) / 196 (sync/エラー地) | zoom 表示・sync 枠・診断 Error |
| アクティブ枠 | 緑 `#00ff00` | 46 | pane-active-border・ACTIVE 帯 |
| 警告 | `#d78700` | 172 | 診断 Warn 地 |

fade をバイオレットへ移行した後の ramp サンプル:
201 (#ff00ff 残光) → 164 (#d700d7) → 127 (#af00af) → 90 (#870087) → 53 (#5f005f 闇へ) → 消灯。
色言語の全体像: **橙 = いま (自分がいる場所)** / **紫 = さっきまで (残光が冷める)** /
**シアン = イベント (bell・メッセージの ping)** / 赤 = 危険 / 緑 = 稼働中。

## リスク / 未決定事項

- **fade バイオレットの衝突管理**: hot 201 が現行のシステム magenta 家族と同色のため、
  #7c の衝突解消 (メッセージ系のシアン化) と**必ずセットで**適用する。blink/scratch の 201 を
  残す場合、prefix armed 中や scratch 内では「点滅の 201」と「bucket0 window の 201」が
  瞬間的に並びうる — 実運用で紛れるならそちらを 213 へ (ライブ判断項目)。
  発見性は hot 201 (最大彩度のマゼンタ寄り紫) が島 coral と距離 213 で十分立つ
- **Coral 173 は 199 より地味**: 現在地の視認性が下がる可能性。ライブで物足りなければ
  代替候補: 209 (#ff875f 明るめ)・166 (#d75f00 濃いオレンジ)。定数 1 箇所で試せる
- **窓の active/inactive 対比の反転** (#5): 「アクティブ=warm 地 / 非アクティブ=無彩 234」は
  現行の「アクティブ=紺」より控えめ。物足りなければ非アクティブ側を 236 に上げて差を作る
- **Terminal.app プロファイルは repo 外の状態**を持つ (書き出しで mac/ に保存して緩和)。
  truecolor 端末 (別マシン) では層1 が無いので、truecolor 側の warm 地は gruvbox の
  `#282828` のまま (これも warm。差は許容) か、Phase 2 で Normal bg を #1F1E1D に上書き
- ⚠️ このテーマ変更は見た目の大改修。**Phase 1 だけ入れて数日運用 → Phase 2 を判断**を推奨

## 関連

- [docs/theme-colors.md](../docs/theme-colors.md) — 色の意味マップ・変更手順・ペア表 (適用後に更新)
- [docs/tmux-window-fade.md](../docs/tmux-window-fade.md) — fade の設計判断 (シアン採用の経緯)
- `nvim/lua/dotfiles/palette.lua` / `_tmux.conf` の `@cur-accent` — 定数の実体
