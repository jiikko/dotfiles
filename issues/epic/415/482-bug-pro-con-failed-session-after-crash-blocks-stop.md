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

- [ ] `failed` + pid 無しを止まった形にする / テストに 1 行
- [ ] 変異 (許可リストから外す) で red を確認
- [ ] 本番の dispatcher を起動し直して、5 本の止め直しが止まることを確認
