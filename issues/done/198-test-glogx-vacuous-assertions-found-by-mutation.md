# 198 test: glogx のテスト 5 箇所が変異させても green (何も守っていない)

起票日: 2026-09-03
出典: `/audit` の test-cleanup (direct、2026-09-03)
重要度: P2 (発見 1 と 4 は実バグを見逃す形。他は P3)
関連: [`_claude/rules/mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md)

## 前提

**全件、実際に変異を当てて green を確認している** (推測ではない)。baseline は
`go test ./...` 全 green (31 秒) を先に測ってから当てた。**発見 1 と 4 は main agent が
独立に再現した** (下記に再現時刻を書く)。作業ツリーは復元済み。

## 発見 1 — `usage_overlay_test.go:TestOverlayBoxTopRightEmpty` (assert が無い)

```go
_ = overlayBoxTopRight(window, nil, 10, false)          // 空 box: 何もしない
_ = overlayBoxTopRight(window, []string{"x"}, 0, false) // width0: panic しなければ OK
```

3 文中 2 文が戻り値を捨てており、「何もしない」という主張を一切していない。
守るはずの対象は `usage_overlay.go:overlayBoxRight` の早期 return ガード。

- **変異** (2026-09-03 に main agent が再現): `overlayBoxRight` の
  `if len(window) == 0 || width <= 0 || len(box) == 0 { return window }` を `return nil` へ。
  ビルドは通り、`go test .` は **green のまま** (31.2 秒)
- **実害**: 「box が空 / 幅 0 のとき背景を丸ごと落とす」= 画面が消えるバグを誰も検出しない
- **対応**: 書き直す。`got := overlayBoxTopRight(window, nil, 10, false)` を受けて
  `slices.Equal(got, window)` を見る。コメントの「何もしない」がそのまま assert になる

## 発見 2 — `issues_view_test.go:TestIssuesViewBodyVisibleRows` (差が出ない入力)

期待値 `max(o.page-len(...), 1)` を `v.visibleRows` と突き合わせるが、幅 24/40/84/200 ×
`page: 20` のどのケースでもヘッダ行数が page を下回るため、**下限クランプ `max(..., 1)` が
発火する状況を作っていない**。

- **変異**: `issues_view.go:visibleRows` のクランプを削除 (`vp.page-len(...)`) → 全体 green のまま
- **実害**: 0 行や負の行数がキー処理へ流れる経路が無検査
- **対応**: `page` を極小 (1〜3) にしたケースを 1 本足す。テスト本体は有効なので削除は不要

## 発見 3 — `doctor_view_test.go:TestDoctorReusesHeavyEntries` の `nomeasure` ケース

6 ケース中 `nomeasure` (`MeasuredAt = time.Time{}`) は `doctorReuseFrom` の
`r.MeasuredAt.IsZero()` を守るつもりのケースだが、**ゼロ値は直後の `age := now.Sub(...)` で
必ず `age >= doctorHeavyReuseTTL` になる**ので `old` ケースと同じ経路に落ちる。

- **変異**: フィルタから `|| r.MeasuredAt.IsZero()` を削除 → `go test -run TestDoctor` green のまま
- **対応**: **ケースは残してよいが「守っている」と数えない**。production 側の `IsZero()` は
  TTL 条件に包含された冗長条件なので、production を削るか、テストコメントから
  「ゼロ値を弾く」という主張を落とす。現状は「6 条件を守っている」ように見えて実際は 5 条件

## 発見 4 — 色定数の自己言及 (package 全体)

`if !strings.HasPrefix(out[0], ansiYellow)` のように、**期待値が production の `render.go` の
定数そのもの**。定数を変えると期待値も一緒に動くので永久に green。

- **変異** (2026-09-03 に main agent が再現): `render.go:20` の `ansiYellow = sgr.Yellow` を
  `sgr.Cyan` へ。ビルドは通り、`go test .` は **green のまま** (31.2 秒)
- **実害**: commit 行と hunk 行が同じ色になる (UI の意味的退行) を 957 本のテストが 1 本も検出しない
- **対応**: 全部を生エスケープにする必要は無い。`highlight_test.go` に
  **「commit 行の色 ≠ hunk 行の色 ≠ 追加行の色」という定数どうしの相互不一致**を 1 本足せば
  この変異クラスが red になる (そこだけ生の `"\x1b[33m"` で pin する形でも可)

## 発見 5 — `TestDropEmojiVS16` が main と termsafe で重複

`terminal.go:dropEmojiVS16` は `termsafe.DropEmojiVS16` への 1 行 alias で、main 側テストの
3 assert (VS16 除去 / bare 記号保持 / VS16 無しは素通り) は termsafe 側と 1:1 で同じ主張。

- **変異**: `termsafe.DropEmojiVS16` を `return s` へ → `glogx/termsafe` と `glogx` の
  **両方が同時に FAIL** (1 変異で 2 本落ちる = 片方が冗長であることの実証)
- **対応**: main 側の重複 3 assert を削除。ただし `dispWidth("🚨") == 1` と VS15 保持の
  2 assert は main/termwidth 固有なので残す。同名テストは紛らわしいので残す分を
  `TestDropEmojiVS16BareWidth` 等へ改名する

## 受け入れ条件

- [ ] 発見 1〜5 それぞれについて、上記の変異を当てると **red になる**ことを確認する
- [ ] 発見 3 は「production 側の冗長条件を削る」か「テストの主張を訂正する」かを決めて記録する
- [ ] 直したテストは変異を戻して green に戻ることも確認する

## 攻めたが見つからなかった範囲 (次の監査の起点)

- `vs16_literal_test.go` / `waitdelay_discipline_test.go` (ソース走査型): 後者は `checked == 0` で
  走査崩壊を FAIL にする guard を持ち堅い。前者は同 guard が無く「パース対象 0 件で green」に
  なりうるが、**production 側の変異で発火させられなかった**ので報告に留める (発火には移動や
  ビルド構成の変更が要る)
- `doctor_view_test.go` 全 32 本: `TestDoctorStartupToast` は架空 ID・Total 詐称・cooldown 未来値など
  vacuous 化の罠に先回りしており (commit `302aa45a` の跡)、属性の存在だけを見る assert は無かった
- `tui_overlay_test.go` / `tui_panel_test.go` / `tui_nav_test.go`: `!= ""` 系 30 箇所を確認。
  いずれも positive assert と対の negative assert で、片肺のものは無し
- 環境ゲート: `t.Skip` 22 箇所を全確認。fsnotify/symlink/git 不在は「作れるのに張っていない」を
  FAIL に分ける形で、黙って緑になる skip は無い
- fake 群 (`fakeWatcher` / `stubUpdates` / `stubDiff`): 常に成功する fake は無く、exit code は
  `usage_overlay_test.go` の実 stub CLI (`exit 1`) で模されていた

## 対応 (2026-09-03、後続セッション)

**5 件すべて修正し、各件で変異 → red を確認した** (production は 1 行も変えていない。変更は
テストファイル 5 本だけ)。

| 発見 | 修正 | 当てた変異 → 結果 |
|---|---|---|
| 1 | `TestOverlayBoxTopRightEmpty` で戻り値を `slices.Equal` と突き合わせる | `overlayBoxRight` の早期 return を `nil` へ → **red** |
| 2 | `TestIssuesLayoutAgreesBetweenKeysAndRender` に「page がヘッダー行数以下でも 1 行は残す」を追加。期待値は式でなく**リテラルの 1** | `visibleRows` の下限クランプを外す → **red** |
| 3 | `TestDoctorReuseSkipsZeroMeasuredAtNearEpoch` を新設 (純関数 `doctorReuseFrom` を直接呼ぶ)。既存の `nomeasure` ケースには「TTL に包含されていて守っていない」と注記 | `r.MeasuredAt.IsZero()` を外す → **red** |
| 4 | `TestAnsiColorsAreDistinctPerMeaning` を新設。基本色をリテラルで pin + 意味の違う色どうしの衝突を検出 | `ansiYellow = sgr.Cyan` → **red** |
| 5 | main 側の重複 3 assert を削除し、残りを `TestDropEmojiVS16BareWidth` へ改名 | `DropEmojiVS16` を無効化 → termsafe 側と main の `TestSanitizeDetailLineDropsVS16` が **両方 red** (カバレッジは失われていない) / `termwidth.Of` が bare ⚠ を 2 と数える → `TestDropEmojiVS16BareWidth` が **red** |

### 途中で踏んだ誤り (同じ罠を次に踏まないため)

発見 3 の新テストは、最初 `now := time.Unix(0, 0).Add(30 * time.Minute)` と書いたため
**変異を当てても green のままだった**。`time.Time{}` のゼロ値は **Unix epoch (1970) ではなく
西暦 1 年**で、`time.Unix(0,0)` を基準にすると差が約 2,562,047 時間になり TTL 判定で弾かれる。
`time.Time{}.Add(30 * time.Minute)` に直して red を確認した。テスト本文にもこの注記を残した。

**「テストを書いた」だけでは守っていない**ことの実例で、変異検証を通さなければ
issue 198 が指摘したのと同じ vacuous なテストをもう 1 本増やすところだった。

### issue 本文の誤り (訂正)

発見 2 のテスト名を `TestIssuesViewBodyVisibleRows` と書いていたが、実際は
`TestIssuesLayoutAgreesBetweenKeysAndRender` (その中のサブテスト「本文 (引き出し)」)。

### 受け入れ条件

- [x] 発見 1〜5 それぞれについて、変異を当てると red になることを確認した
- [x] 発見 3 は「production の冗長条件を削る」ではなく「テストで守れるようにする」を選んだ。
      `IsZero()` は now が epoch 近傍のとき (fake clock / 壊れた snapshot と時計のズレ) に
      **TTL では代替できない**ため、外さずに残した
      ([`list-masked-failure-modes-before-removing-guard.md`](../../_claude/rules/list-masked-failure-modes-before-removing-guard.md))
- [x] 変異を戻して green に戻ることを確認した (`go test ./...` green / `make lint` 0 issues)
