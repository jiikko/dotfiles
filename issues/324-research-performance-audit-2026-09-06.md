# research: performance 監査の記録（2026-09-06）— 全数勘定・却下理由・未決着

起票日: 2026-09-07
カテゴリ: research
出典: `/audit` performance / forge Minimum+（7 体・約 31 分）。
🚨 **1 回目の実行はセッション上限で 4 体とも起動失敗**（`integrated: null`）し、上限リセット後に回し直した

resource-leaks は [issue 308](308-research-resource-leaks-audit-2026-09-06.md)、
dead-code / broken-code は [issue 318](318-research-dead-code-and-broken-code-audit-2026-09-06.md)。

## 全数勘定

| | 件数 |
|---|---|
| 実測つきで確定 | 22（High 2 / Medium 12 / Low 8） |
| 重複除去 | 3 |
| 除外 | 2 |
| 未決着（両論併記） | 4 |
| **issue 化** | **319 / 320 / 321 / 322 / 323** |

## この監査の収穫（実測が揃った 3 点）

1. **`go_autobuild_exec` が指紋を 2 回計算**（3.83 ms/起動、変異検証済み）→ issue 319
2. **doctor フレームが `doctorMaxMarkWidth()` を行ごとに再計算**。`sync.OnceValue` 化で
   **-19,948 B/frame**（フレーム確保バイトの 26%）→ issue 321
3. **precmd コストの 99% は repo 自前ではなく第三者 hook 側**（zsh-autosuggestions / direnv）
   → issue 322。**自前の precmd fork は 0 件**

## 🚨 クロスレビューが潰した事実誤認 3 件（そのまま実装根拠にすると危険）

私も独立に確認した。

| 一次報告の主張 | 実際 |
|---|---|
| 「glogx は forbidigo で `os.Chdir` を禁止済み」 | **誤り**。forbidigo が禁じるのは `^fmt\.Print(f\|ln)?$` 系。`os.Chdir` は **errcheck の `exclude-functions`**（= 戻り値エラーを無視してよい）＝**真逆** |
| 「`issues/` に `MarkVocabulary` の言及 0 件」 | **誤り**。`issues/done/238` が 9 箇所で正面から扱っている（「語彙から導出する形へ直した」制約つき） |
| 「`issues-40` だけが予算の境界」 | **不完全**。`-race` では `job-panel` も +2 しか余裕が無い |

1 つ目は特に危険で、**「lint で強制されているから安全」という根拠でキャッシュを入れると、
実際には何も強制されていない**（issue 320 に転記済み）。

## 却下した指摘（理由つき。再生成防止）

### ① `direnv` の precmd fork を `DIRENV_WATCHES` の突き合わせで飛ばす

❌ direnv の**内部表現に依存**する。ツール側の更新で無言で壊れ、しかも壊れ方が
「環境変数が反映されない」という**気づきにくい形**になる。
取れる手は「precmd から外して chpwd + 起動時 1 回にする」方向だけ（issue 322 ②）。

### ② `copyText` / `copyPath` の毎フレーム構築を新規 issue として起票する

❌ 重複。既存の枠組みで扱える範囲。

## 未決着（両論併記。着手時に判断）

1. **direnv の per-prompt コストの代表値と計測条件** — 対話シェル内 4.14〜4.79 ms と
   standalone 5.14〜5.38 ms のどちらを代表値にするか。**測った経路がユーザーの実経路と同じか**の問題
   （[`perf-claims-need-measurement.md`](../_claude/rules/perf-claims-need-measurement.md)）
2. **`disk/guard.go:excludedRootFor` を直すか据え置くか** — path 1 本ごとに 10 root を
   `EvalSymlinks` し直しているが、これは**破壊的操作のガード**の一部。
   正規化結果のキャッシュは **TOCTOU の窓を広げる**
   （[`sandbox-real-destructive-test-apis.md`](../_claude/rules/sandbox-real-destructive-test-apis.md)
   の「実行の直前に取り直した値で判定する」）。走査（読み取り）と削除（破壊）で扱いを分ける案がある
3. **`loadWorktreeStatus` の root キャッシュの置き場所** — パッケージ変数（`t.Chdir` を使う
   既存テスト 3 本と衝突 / `-race` で落ちる）か、view が持つか（issue 320 に転記済み）
4. **zsh-autosuggestions / `doctorMaxMarkWidth` の severity** — 一次報告は両方 high。
   クロスレビューが medium へ落とした（前者は効果が知覚閾値に届かない、後者は
   doctor が常時 60fps ではない）。**着手順は変えず、severity 表記だけ下げる**で決着

## 攻めたが 0 件だった範囲

- **zsh hook の自前 fork**: 0 件（`$(...)` を precmd / preexec から呼ぶ形は repo 内に無い）
- **tmux status の再描画**: 0 件
- **doctor 走査の直列化**: 0 件
- **glogx のフレーム O(N²)**: 0 件

🚨 **ただしこの 0 件宣言はクロスレビューで追認されていない**（監査自身が low 項として明記した）。
[`CLAUDE.md`](../CLAUDE.md)「不在の主張は数え直す」に照らすと、
**次の performance 監査はこの 4 項目を最初に再確認するところから始めるべき**。

## 起票しなかったが記録に残すもの

- **`bin/lib/go_autobuild.zsh` の pid 数値ゲートが同一ファイル内で非対称**:
  `_go_autobuild_take_lock`（281 行）にゲートが無く、509 行にはある。
  [`shell-numeric-gate-explicit-digits.md`](../_claude/rules/shell-numeric-gate-explicit-digits.md)
  の対象だが、入力源が `$$` なので発火条件を示せない。**触る機会があれば揃える**
- **`scripts/` の同型 preamble 6 本**（`tmux_agent_panel.sh` / `tmux_fzf_pane_move.sh` /
  `tmux_agent_jump.sh` / `tmux_fzf_jump.sh` / `tmux_resurrect_debounced_save.sh` /
  `tmux_schedule_keys.sh`）: 一括置換の前に**用途で分類が要る**（tmux hook から毎回呼ばれるものと、
  ユーザー操作でしか呼ばれないものが混ざっている）
- **`tmux` の `after-select-pane` hook が pane 移動のたびに 4 fork の preamble を払う**
  （`tmux_agent_panel.sh:cmd_unfocus`）。パネルを使っていなくても全額。上の分類とセットで扱う
- **どのベンチも `etaBasis` を通っていない**: ベンチ fixture が `StartedAt` を設定しないため
  `running()` が常に偽。`tui.go:etaBasis` / `jobTimeSuffix` は測られていない
- **`MarkVocabulary` を disk module 側でキャッシュしない却下理由が噛み合っていない**:
  理由が書かれてはいるが、指している制約が現在の実装と対応していない（issue 321 で触るときに直す）
