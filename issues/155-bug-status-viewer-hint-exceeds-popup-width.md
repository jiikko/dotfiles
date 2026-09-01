# 155 bug: status viewer の hint が popup 実幅で末尾ごと切れている (R/s/q が見えない)

起票日: 2026-09-01

`statusView.hint()` の一覧モードの文字列は **155 桁** (`status_view.go:hint`)。glogx を tmux
popup で使うときの実幅は 84 桁前後 (`tui_helpers_test.go:testPopupWidth = 84`) で、hint は
`browseModel.hintLineText` (`tui.go`) が **末尾から黙って切り捨てる** (`clipToWidth`)。

## 実測 (2026-09-01)

幅 82 (フレーム有効時の clip 幅 = `m.width-2`) で見えるのはここまで:

```
j/k: 移動  Tab: セクション  Space: stage/unstage  a: 全 stage  X: 変更を捨てる  d:
```

切れて**画面に出ないキー**:

```
 diff  r: 再読込  b: push  p: pull  U: usage  R: 残量  s: 一覧へ  q: 終了
```

つまり **status viewer から抜ける手段 (`s` = 一覧へ / `q` = 終了) が案内から消えている**。
`b`/`p` (push / pull) と、2026-09-01 に足した `R` (ratelimit ダッシュボードへの横断、`d01c52a`)
も同様に見えない。

## なぜ issue にするか (非対称)

issues viewer 側には **`TestIssuesViewHintFitsPopupWidth`** (`issues_view_test.go`) があり、
`popupWidth = testPopupWidth` を超えたら落ちる。実際そのテストがあるおかげで、issues 側は
「`i: 一覧へ` は入れられない」と判断してキーを削り、案内を `--help` と README へ寄せている
(`issues_view.go:hint` のコメントが正本)。

**status viewer にはその制約が無く、テストも無い**。同じ repo の同じ種類の画面で、片方だけ
無検査なので、キーを足すたびに末尾が静かに削られていく (今回の `R` 追加がまさにそれ)。

## 対応案 (どれを採るかは未決定)

1. **status 側にも幅テストを足し、入らないキーを削る** (issues 側と同じ解決。削ったキーは
   `--help` / README / `docs/status-viewer-spec.md` を正本にする)。⚠️ 削る候補を選ぶ判断が要る
   — 抜ける手段 (`s`/`q`) を残すのが最優先で、`Space`/`a`/`X` の説明語を短縮する余地がある
2. **hint を幅で切らずに畳む** (入らない分を `…` にする / 2 段に分ける)。全画面ビューなので
   最下行を 2 行にする余地はあるが、issues viewer と行数の契約が変わる
3. **幅に応じてキーを落とす** (広い端末では全部、狭いと必須だけ)。実装が最も重い

## 関連

- `d01c52a` — `R` (ratelimit ダッシュボードへの横断) を hint に足した commit。この issue の
  発見契機。⚠️ `TestStatusRSwitchesToRatelimitDash` は `hint()` の**文字列**に `R: 残量` が
  入ることしか見ておらず、**画面に出るか**は見ていない (この issue が直せば意味を持つ assert)
- `_claude/rules/no-mixed-width-columns-in-terminal-ui.md` — 「桁は合っているが目には合わない」
  同族。こちらは「桁も合っていない」形
