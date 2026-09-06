# test: 性能ゲートの予算が、名指ししている退行を観測できない（4 形）

起票日: 2026-09-07
カテゴリ: test
優先度: 中（予算は在るが**壊れても緑**。issue 269 と同じ「観測できない予算」の再生産）
出典: /audit performance 2026-09-06（forge Minimum+）

## ① zsh の予算が実測の 12〜20 倍緩い

`tests/zshrc/bench_budgets.ci` の `prompt_lag` / `startup` / `first_command` の上限が
実測値から大きく離れており、**6.7 ms 級の退行を構造的に観測できない**。

実例: issue 322 ① の `ZSH_AUTOSUGGEST_MANUAL_REBIND` は precmd を 12.00 → 5.31 ms にするが、
通しの計器では BASE 26.0 / 24.2 / 24.7 vs FIX 18.7 / 24.1 / 22.2 と**分布が重なって判定不能**。
**改善も退行も見えない。**

## ② doctor のフレーム予算は 4 タブ中 `tabDisk` 1 つだけ

`src/glogx/frame_alloc_test.go:budgetDoctorModel` は disk タブだけを測る。
**削除パネルと CI の doctor metric は 0 件**。

## ③ `issues-40` の予算が余裕 0

```go
{"issues-40", ..., 213, 34900},   // 記録値 209 + 標準の余裕 +4
```

監査の実測では現在値が 213 に達しており、**記録値 209 からの +4 の退行が予算内に吸収されている**。
🚨 **私はこの再測をしていない**（`-race` の測定に時間がかかるため）。
**着手時に必ず測り直すこと**。同じ測定で `job-panel` も `-race` では +2 の余裕しかない、
という報告があるので併せて確認する。

## ④ `doctor-disk` の予算が `-race` 基準で置かれている

```go
{"doctor-disk", budgetDoctorModel, 620, 86000},
```

コメントが明記しているとおり、これは **`-race` の観測レンジ（597〜601）に ~3%** の上限。
`-race` は allocation を増やすので、**非 -race で走らせると 25% / 17% の悪化を素通しする**。

## 共通の構造

どれも「予算は在るが、その予算が**何を検出できるか**を測っていない」形。
[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)
の「その機構を外したら観測結果は変わるか」に yes と答えられない。

## 推奨対応

1. **各予算について「どれだけ悪化したら red になるか」を実測して書く**
   （上限だけでなく、**現在値と上限の比**をコメントに残す）
2. ①: zsh の予算を実測レンジへ締め直す。分解能が足りないなら
   **計器自体を分解能の高いもの（precmd 1 サイクルの直接計測）へ変える**
   — [`perf-claims-need-measurement.md`](../_claude/rules/perf-claims-need-measurement.md) の
   「比・伸び率で測る」も検討する（runner 速度差が打ち消える）
3. ②: doctor の残り 3 タブと削除パネルを予算に入れる
4. ③④: **測定条件（`-race` の有無）を予算と同じ場所に固定する**。
   条件が違う数字を 1 つの上限で受けない
5. **変異検証**: 各予算について「その予算が守っている最適化を外す」変異を当て、
   **ケースごとの pass/fail 一覧**で red を確認する
   （スイートの rc で読むと他ケースの red に隠れる）

## 受け入れ条件

- [ ] ③の現在値を**実測し直す**（本 issue は監査の実測を引用しているだけ）
- [ ] 各予算に「現在値 / 上限 / 測定条件」が揃っている
- [ ] 変異検証をケース名ごとの一覧で確認した記録が commit message にある

## 関連

- `issues/done/269`（bench の時間予算が退行を観測できない、という同型の先行 issue）
- issue 321（doctor の予算を締め直す作業。同じ commit でもよい）
- issue 322（この計器で見えなかった改善）
