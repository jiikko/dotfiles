# 409 (bug): lockman の `--on-lost` が値検証されておらず、綴り間違いが黙って kill になる

起票日: 2026-09-21
出典: [359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) の残タスク (最後の 1 件。切り出して 359 を done にした)

## 概要

`--on-lost` は `kill | warn` の 2 値だが、**判定が `!= "warn"` の否定形**なので、
`--on-lost=warm` のような綴り間違いが**黙って kill 扱い**になる。
[091](done/091-feat-lockman-directory-lease-lock.md):282 は `--on-lost=kill|warn` と明記しており、
未知の値の扱いは規定していない。

現状 (2026-09-21 に実コードで確認):

- `main.go:124` — `fs.StringVar(&o.onLost, "on-lost", "kill", ...)` で受けるだけ
- `main.go:306` — `runWith(l, o.ttl, o.label, o.onLost != "warn", child)`

**黙って倒れる向きが危険側**なのがこの issue の本体。`warn` のつもりで `warm` と打った人は
「lease を失っても子は走り続ける」と思っているのに、実際は SIGTERM → 猶予 5s → SIGKILL
(`with.go` の `onLostGracePeriod`) が撃たれる。

## 対応方針

`--io-timeout` の値検証 (357 で入れた `main.go:197` の形) と**同じ場所・同じ形**で弾く。

```go
if o.onLost != "kill" && o.onLost != "warn" {
    warnf("--on-lost が不正 (%q)。kill | warn のどちらかを指定すること", o.onLost)
    return failCode(cmd, exitError)
}
```

- `failCode` を通すこと (`with` では `exitError` を 125 = `exitWithInvalid` へ寄せる。`main.go:385`)
- **検証は subcommand を問わず無条件で行う**のを推奨。`acquire` 等では `--on-lost` は読まれないが、
  そこで黙って受けると「効いたつもり」を作る。狭めるなら `cmd == "with"` に限る判断もありうるので、
  実装時にどちらかを選んで理由をコード直近へ書く

## 受け入れ条件

- [ ] `kill` / `warn` 以外を渡すと `with` は rc=125、それ以外の subcommand は rc=1 で止まる
- [ ] エラー文に**受け付ける値**が出る (人がその場で直せる)
- [ ] `kill` / `warn` の正常系が退行していない (既定 `kill` を含む)
- [ ] 変異検証: 足した検証を外す (= `!= "warn"` のままに戻す) 変異で、**このテストだけが** red になることを
      package + テストケース名で確認する ([`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md))

## 着手前のメモ (2026-09-21 の反証レビューで確認済み)

- **先例テストがそのまま雛形になる**: `src/lockman/io_timeout_test.go` の
  `TestIOTimeoutValueIsValidated` が「`check` で rc=1 / `with` で rc=125」を
  テーブルで固定している。同じ形で書ける
- `--on-lost` の**値検証・enum チェックはコード中に 1 つも無い** (`main.go` / `with.go` / `lock.go` の
  全数 grep + 既存テスト `on_lost_kill_test.go` / `on_lost_deadline_test.go` / `main_test.go` を確認)
- **フラグ登録は subcommand 分岐の外**なので、`acquire` 等でも `--on-lost` は無条件にパースされて黙って受理される
- 上の提案パッチを置く区間 (`main.go` の `--io-timeout` 検証 〜 `NewLocker` 呼び出しの間) に**到達不能にする早期 return は無い**
- **typo 単体が即 kill を起こすわけではない** — lease 喪失 / 判定不能が実際に起きたときに kill 側の挙動になる

## 関連

- [357](done/357-bug-lockman-with-bypasses-io-timeout.md) — `--io-timeout` の範囲検証を入れた先例 (同じ族)
- [385](done/385-design-lockman-on-lost-kill-vs-keep-renewing.md) — `--on-lost` の意味論を決めた issue (値の集合はここで確定している)
