# `TestStaleTakeoverHasExactlyOneWinner` が低頻度で「勝者 2 人」になる (原因未特定)

起票日: 2026-09-12
カテゴリ: bug / priority: **high**（主張の重さによる。実害の有無は未確定）
対象: `src/lockman/lock.go` の `tryTakeover` / `src/lockman/lock_test.go:88` の同テスト
出典: [issue 358](358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) の敵対レビュー 5 周目 (観点②) と、7 周目の作業中に再観測
反証レビュー: 未実施

## 問題

**「期限切れの引き継ぎを同時に狙っても、引き取れるのは 1 人だけ」**という
lockman の中核の不変条件を測るテストが、低頻度で `引き継ぎの勝者が 2 人 (期待 1)` で落ちる。
テスト自身のコメントが「ここが二重取得の最頻出経路」と書いている面。

**これがテストのハーネス由来なのか production の race なのかは未特定**。
主張の重さから priority: high としているが、**実害が確定したわけではない**。

## 実測 (2026-09-12 / darwin arm64 / go1.25.4)

単独実行 (`-run 'TestStaleTakeoverHasExactlyOneWinner$'`):

| 条件 | 失敗 |
|---|---|
| HEAD (`2c474a4d`)、`-count=100`、**他のエージェントが並行実行中** | **5/100** |
| HEAD、`-count=300`、静穏 | **0/300** |
| 5 周目より前 (`7ca2533b`)、`-count=100`、静穏 | 0/100 |
| 5 周目より前、`-count=300`、静穏 | **1/300** |
| HEAD、**`-race`** `-count=100` | 0/100 |
| full suite `-count=1` ×8 (静穏) | 0/8 |

観測された値はすべて「勝者が 2 人」(3 人以上は出ていない)。

**独立の観測**: issue 358 の 5 周目 (観点②) のレビュワーが、full suite 約 50 回のうち
2 回の原因不明 FAIL を観測し、うち 1 件がこのテストだった (並列実行中)。

## 分かっていること

- **issue 358 の掃除機構の変更が原因ではない**。`Acquire` は `Cleanup` を呼ばない
  (`grep -n 'Cleanup(' lock.go` が 0 件) ので、このテスト中に 358 で触ったコードは走らない。
  **両腕 (HEAD / 5 周目より前) の双方で再現した**
- **負荷に感応する**。並行して他の重いプロセスが走っているときに率が上がる
  (5/100 が出たのはその条件。静穏では 400 回中 1 回)
- **`-race` では出ない** (400 回相当で 0)。検出器が遅くする方向に効いて窓が閉じる

## 未特定 (次に見るべきところ)

- fixture の `ttl := 50 * time.Millisecond` + `time.Sleep(3 * ttl)` が、
  `serverNow()` の mtime 粒度 (サーバ側の打刻) に対して十分かどうか。
  粒度が 1 秒の FS では「期限切れ」の判定自体が揺れる
- `tryTakeover` の rename 引き継ぎで、2 つの goroutine が**別々の**期限切れ lock を
  見て両方成功する経路があるか (1 人目が引き継いだ直後の新 lock を、2 人目が
  まだ古い state で見ている窓)
- ハーネス由来なら、判定軸を壁時計から外す
  ([`avoid-wall-clock-assertions.md`](../_claude/rules/avoid-wall-clock-assertions.md))。
  [364](364-bug-lockman-with-release-failure-and-graveyard-retention.md) の 4 番
  (`TestRenewExtendsHold` の壁時計依存) と同じ族の可能性がある

## 再現手順

```sh
cd src/lockman
# 静穏だと 300〜400 回に 1 回。負荷をかけると率が上がる
go test -run 'TestStaleTakeoverHasExactlyOneWinner$' -count=300 ./...
```

## 残タスク

- [ ] ハーネス由来か production の race かを切り分ける (上の「未特定」の 3 点)
- [ ] production の race だった場合の修正
- [ ] ハーネス由来だった場合は判定軸を壁時計から外す
