# 478 (perf): cards.json は終えたカードも持ち続け、dispatcher は空の Tick でも 8 回読み直す (費用が使った日数に比例して増える)

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

リソースリークの監査 (479) で見つけた。カードの記録 `cards.json` から終えたカードが消えないのは、438 の「片付けは画面から外すだけ (記録からは消さない)」の決定どおり。
ただ、上限・回し・書庫への移し替えが無いまま、dispatcher と画面が毎回ファイル全体を読んで JSON を解く。
そのため費用は、今動いているカードの数ではなく、これまでに作ったカードの総数に比例して増え続ける。

## 詳細

- 該当: `src/pro-con/store/store.go` の `Load` (`os.ReadFile` → `json.Unmarshal` を全体に) と `writeAtomic` (全体を書き直す)。
  dispatcher の `Tick` (`tickRuns` はテストの係の実行中なら 2 度、`stopMarked`・`pm.go` ほか)。画面は `live.Backend.refresh` で 3 秒ごと + dispatcher に知らされるたびに読む。
  取り除くのは削除の依頼 (451) だけ。`Applied` / `Rejected` は `keepApplied` で上限がある
- 発火条件: 使った日数。2026-09-25 の dogfooding では 1 日で 15 枚・57KB (1 枚あたり約 4KB。本物の cards.json を読んで数えた)。この速さなら 50 日で約 750 枚になる
- 実測 (module の一時コピーで。本物の cards.json の写しを使った):
  - 空の Tick 1 回あたりの `store.Load` の呼び出し: 9 回 (1 回目) / 8 回 (2・3 回目)。コピーの `Load` に数える口を足し、既存の偽物 (`newDispatcher` / `fakeLauncher`) で回した
  - `Load` 1 回の所要 (本物の 15 枚を複製した記録。20 回の平均):

    | 枚数 | ファイル | Load | writeAtomic |
    |---:|---:|---:|---:|
    | 15 | 57KB | 0.5ms | 0.2ms |
    | 150 | 575KB | 3.7ms | 0.2ms |
    | 750 | 2.9MB | 17.2ms | 2.3ms |
    | 3000 | 11.5MB | 70.0ms | 4.0ms |

  - 750 枚なら、Tick ごとの読み直しだけで 3 秒あたり約 140ms (1 コアの約 5%)。3000 枚なら約 560ms (約 19%)。これに開いている画面ごとの読み直しと、状態が変わるたびの全体の書き直しが加わる
- 漏れたとき: 何も壊れないまま、dispatcher と画面の CPU・電池と、Tick の遅れ (知らせてから画面に出るまで) が日ごとに増える。人が気づくきっかけが無い
- 意図の反証: 438 は「記録からは消さない」と決めている。これは履歴 (`card show`・449 の起動時刻の集計) を失わないための決定で、読み直しの費用には触れていない。
  events.jsonl には `MaxBytes` の回しがあり、runs/・sessions-retired.json・inbox/rejected/ が増え続けることは 460 に既知。cards.json はどちらにも入っていない

## 対応方針 (候補)

- 終えたカード (完了・片付け済み) を、一定時間後に書庫 (`cards-archive.jsonl` のように足していくだけのファイル) へ移す。Tick と画面は動いているカードだけを読む。
  `card show` と履歴の集計は書庫も読む (438 の「記録からは消さない」を守る)
- 1 Tick の中では 1 度読んだ State を渡し回す (8 回を 1〜2 回に)。書き手は dispatcher だけなので、Tick の中では読み直さなくても食い違わない
- 性能の予算: N 枚の記録での Tick の所要を bench にする (直す前後を同じ bench で測る)

## 進捗

- [ ] 設計 (書庫へ移す条件と、読む側の一覧)
