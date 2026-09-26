# 482 (bug): マシンのクラッシュ後、pid 無し・failed の session を「止まったと言えない」と読み、--stop が止め直しを続ける

起票日: 2026-09-26

## 概要

2026-09-25 23:53 の macOS カーネルパニック (原因の調査は別。下の「関連」) の後、`claude agents --json --all` は
クラッシュ前の PG / PM の session を **`state: "failed"`・pid 無し**で出す。pro-con の `Session.Stopped()` の許可リストは
`stopped` / `done` だけなので、これを `UnknownState` (issue 466) と読み、`pro-con dispatcher --stop` が

- 「pid が無く state が "failed" (知らない値) なので、止まったと判定できない。止めに行く」→「まだ動いていたので止め直した」

を 5 本ぶん延々と繰り返し、rc=1 (`5 本が止まっていない`) で返った。dispatcher も抜けない。

## 実測 (2026-09-26、claude 2.1.282)

- 再起動から 30 分以上たっても、5 本 (C-025 408eb1a4 / C-026 d37afe7e / C-027 afc49aa4 / C-029 d3102362 / PM fdaa56ca) は
  pid 無し・`failed` のまま。自動では再開しない
- 同じ一覧で、dispatcher が再起動後に起こして `--stop` で止めた 2 本 (090f3b7f / ad277024) は `stopped`
- 425 の実測の `failed` は pid あり (API エラーで turn が落ち、プロセスは生きている)。pid 無しの `failed` は今回初めて見た形

## 対応方針

- `Stopped()` の許可リストに `failed` を足す (pid 無しのときだけ)。pid ありの `failed` は従来どおり止まっていない
- 466 の「知らない state は止めに行く側」は変えない (`failed` は知っている state になるだけ)

## 関連ファイル

- `src/pro-con/agents/agents.go` の `Stopped` / `UnknownState`
- `src/pro-con/agents/agents_test.go` の `TestSessionStopped`

## 関連

- 466 (知らない state を止まったと読まない) — この判定の出典
- 483 (クラッシュからの復旧の口) — 今回の復旧を手でやって詰まった箇所

## 進捗

- [x] `failed` + pid 無しを止まった形にする / テストに 1 行 (「pro-con: pid 無しの failed を止まった session と読む」)
- [x] dispatcher の段でも固定: `TestShutdownTreatsFailedWithoutPIDAsStopped` (ListAll に failed が残っても Shutdown が抜ける。
  「pro-con: クラッシュ後の failed の PG で終了が抜けることを dispatcher の段で固定する」)
- [x] 変異: `Stopped()` の許可リストから failed を外すと、`TestSessionStopped` の `state=failed pid=0` と
  `TestShutdownTreatsFailedWithoutPIDAsStopped` (Shutdown がエラーで返る) の 2 本が red。戻して green
- [x] 敵対レビュー (opus 1 本): 壊せなかった。呼び出し元の全数で挙動が変わるのは `stoppable` 経由 (ensureStopped / stillAlive /
  unregistered) と pm.go / e2e.go の skip だけで、requeueVanished (gone → stopTarget) は `Stopped()` を見ないので変わらない
- [x] 本番の dispatcher を起動し直して、5 本の止め直しが止まることを確認 (2026-09-26 01:47 の `pro-con dispatcher --stop`: rc=0 で 1 回で抜けた。クラッシュ前の failed の 5 本を止め直した出来事は 0 件。止めたのはその時に動いていた PG 4 本と PM)

## 未確認リスク (敵対レビューの P2。推測)

- 「pid あり・failed (API エラー)」のプロセスがその後に落ちて pid 無し・failed になり、**しかも Claude Code が自動で再開する**形は
  測っていない。あれば ensureStopped / close の「既に止まっていた」が止めずに通り、後で PG が戻る。窓の形は stopped / done を
  許可リストに入れたときと同じ (今回新しく生まれた型ではない)
- 潰し方: API エラーで failed になった claude を kill -9 し、`claude agents --json --all` の遷移を 1 回測る。
  **trigger**: close / delete の後に PG が戻ってきた報告が出たとき
