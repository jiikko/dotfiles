# glogx の Bubble Tea v2 対応状況 — 上げる前 / 機能を足す前に読む

`src/glogx` は **`charm.land/bubbletea/v2` v2.0.8** で動いている (2026-07-25 に v1.3.10 から移行、`ec14a77`)。
このドキュメントは「移行で何が変わったか」「v2 の新機能のうち何を採らなかったか (なぜ)」「次に上げるとき何を測り直すか」を残す。
コードの What はソースが出典。ここに書くのは実装から読み取れない判断と、検証しないと壊れる前提。

## module path の罠

canonical path は **`charm.land/bubbletea/v2`**。`github.com/charmbracelet/bubbletea/v2` にもタグは存在するが、
その go.mod 自身が `module charm.land/bubbletea/v2` を宣言しているので、github パスで require すると path mismatch で落ちる。
lipgloss / bubbles の v2 も同様 (`charm.land/lipgloss/v2` / `charm.land/bubbles/v2`) — ただし glogx はどちらも使っていない
(lipgloss は v1 時代に indirect で入っていただけ。UI は自前描画)。

## v1 → v2 で変えた 4 点

| v1 | v2 | 置き場所 |
|---|---|---|
| `View() string` | `View() tea.View` (画面内容 + 端末モードを毎フレーム宣言) | `tui.go` の `View` / 中身は `viewLines()` に分離 |
| `tea.NewProgram(m, tea.WithAltScreen())` | `v.AltScreen = true` (命令的オプション廃止) | `tui.go` の `View` / `RunBrowse` |
| `case tea.KeyMsg` | `case tea.KeyPressMsg` | `tui.go` の `Update` |
| `msg.Type == tea.KeyRunes && len(msg.Runes) > 1` | `[]rune(msg.Text)` | `tui.go` の `Update` (まとめ配送の分解) |

テスト側は `m.View()` → `m.View().Content` の機械置換のみ。キー文字列 (`esc` / `enter` / `ctrl+c` / `G` 等) は v2 でも同一
なので `handleKey` の語彙は無変更 (v2 で space が `" "` → `"space"` に変わるが glogx は space を bind していない)。

まとめ配送の分解は**残してある**。v2 のデコーダは grapheme クラスタ単位で 1 イベントを返すので原理上まとまらないが、
v1 では pty スモークで実測した回帰 (`TestBrowseBatchedRunesKeyMsg`) であり、削って得られるのは 10 行だけなので保険を優先した。

## 幅モデル — v2 でエンジンの計測ライブラリが変わった

`width.go` の不変条件は「glogx の幅計算とエンジンの幅計算が一致すること」。**その相手は bubbletea の実装詳細で、勝手に一致し続けない**:

- v1 (`standardRenderer`) → `charmbracelet/x/ansi`
- v2 (cursed renderer / `ultraviolet`) → `clipperhouse/displaywidth`

移行時に実測して一致を確認した (詳細な表と適用箇所は `width.go` の `dropEmojiVS16` 直上コメントが出典):

| | x/ansi | uniseg | displaywidth | runewidth |
|---|---|---|---|---|
| bare ⚠ (U+26A0) | 1 | 1 | 1 | 1 |
| ⚠+VS16 | 2 | 2 | 2 | **1** |
| 国旗 🇯🇵 | 2 | 2 | 2 | **1** |

**bubbletea を上げたらこの一致を測り直すこと。** 食い違いが出たらそれが桁ズレ (Terminal.app + tmux で 2026-07-24 に報告された揺れ) の再発経路。
端末層まで含めて測るなら、実端末で `go run ./tools/width-probe` を tmux の内と外の両方で走らせる (TTY が要るのでエージェント環境からは測れない)。

## v2 化で実際に得たもの / 得なかったもの

得たもの:

- **ペーストがキー操作として実行されなくなった**。v1 は bracketed paste を `KeyRunes` のまとめ配送で渡すため、
  分解ループが貼り付け文字を 1 文字ずつコマンド実行していた (`b` が混ざれば push 確認が開く)。v2 は `PasteMsg` に分離され、
  glogx は未処理 = 無視。**この経路を塞ぐコードは無いので、`PasteMsg` に case を足すとまた実行され始める**
- 上流の保守線 (v1 は fix が薄くなる側) と、下記「未採用機能」の選択肢

得なかったもの (期待しないこと):

- **性能**: `BenchmarkViewSteady` は v1 36–44µs / v2 39–45µs で誤差内 (`tea.NewView` 由来の +2 allocs)。
  glogx の描画コストは自前の行組み立て側で、エンジン差し替えでは動かない
- **コード量**: `tui.go` は微増 (保険を残したため)
- **ちらつき低減 (mode 2026 / synchronized output)**: v2 は端末を選んで有効化する (`shouldQuerySynchronizedOutput`。
  `TERM_PROGRAM` が `Apple*` は除外)。この環境は `TERM_PROGRAM=tmux` なので照会自体は走るが、
  **実際に有効かは未確認**。体感差も未観測

## 採用していない v2 機能と理由

| 機能 | 判断 | 理由 |
|---|---|---|
| `FocusMsg` / `BlurMsg` で非フォーカス中の tick / usage リフレッシュ停止 | **見送り (2026-07-25 ユーザー判断)** | 実装は小さく (`spinnerActive` と `usageRefreshTick` の再アーム条件に足すだけ)、`focus-events on` も揃っている。CPU 節約になるが今は要らないと判断。必要になったら「popup は modal なので Blur は macOS ウィンドウ非アクティブ時に来る」想定の実機確認から |
| `SetClipboard` (OSC52) で pbcopy 置換 | 採らない | tmux popup では copy-mode に入れず OSC52 が最も不安定な経路。pbcopy 直書きが唯一の取り出し口 (`options.go` / `external_commands.go` の `copyToClipboard` にも理由あり) |
| `RequestCapability` / `ModeReportMsg` / `RequestCursorPosition` で幅の自己診断 | 前借りしない | 測る場所は TUI ではなく単発診断ツール `tools/width-probe` 側。桁ズレが再発したときに入れる |
| `RequestBackgroundColor` でライト/ダーク出し分け | 採らない | tmux 越しに問い合わせが通るか不確実で、ライト背景で使う要望がない。色は `docs/theme-colors.md` の意図で固定 |
| `KeyboardEnhancements` (shift+enter 等) | 採らない | 割当を増やす要望がない。有効化すると離鍵イベントも流れるので、`KeyPressMsg` で受けている今の形が前提になる |
| `ProgressBar` / `WindowTitle` / カーソル制御 | 採らない | 端末対応が前提 (Terminal.app は progress bar 未対応の見込み)。中央モーダル + スピナーの方が確実に見える |
| `tea.WithFPS` 等で自前 tick 置換 | 不可 | 80ms / 33ms の tick は再描画ではなく状態遷移 (`advanceScroll` / `advancePullAnim` / `toast.advance`) を進めている |
| bubbles/v2 コンポーネント (viewport / list / help) | 採らない | パネル・diff・overlay は要望起点の独自挙動 (行を差し込まず重ねる等) が固定されており、置換は複雑性の移動にしかならない |

## 次に bubbletea を上げるときのチェックリスト

1. `go get charm.land/bubbletea/v2@<version> && go mod tidy`
2. **幅モデルの一致を測り直す** (上表。エンジンの計測ライブラリが変わっていないかも確認する)
3. `make -C src/glogx lint` / `make -C src/glogx test` (CI の 2 job と同一コマンド)
4. tmux 上で実 TUI を起動し、最外周フレーム・usage オーバーレイ・`j/k/G/Enter/Esc/q`・Alt Screen 復帰を目視
5. `PasteMsg` の扱いが変わっていないか (貼り付けがキー実行に戻っていないか)

## 他の Go プロジェクトはまだ v1

`src/git-popup` / `src/parallel-each` は `github.com/charmbracelet/bubbletea v1.3.10` + `lipgloss v1.1.0` のまま (v1 系では最新)。
glogx より移行コストが高い: `tea.KeyMsg` / `tea.KeyRunes` の参照が桁違いに多く、`key.Type`/`Runes` → `Code`/`Text`/`Mod` の書き換えに加えて
**space が `" "` → `"space"` になる変化はコンパイルエラーにならない** (静かに壊れる)。上げるなら 1 モジュールずつ、キー操作の目視確認つきで。

なお 3 モジュールが同じ charm 依存を独立に持っているため、バージョンの揃え忘れは構造的に起きる (揃える仕組みは今はない)。
