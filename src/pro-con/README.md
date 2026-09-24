# pro-con

PM (producer) と PG (consumer) を分けて Claude Code を並列に回すための TUI。
**設計の正本は issue 415** (`issues/` 配下の `415-design-claude-pm-worker-orchestration.md`)。ここには実装側の事情だけを書く。

## 現状: ハリボテ (模擬データで動く)

```sh
bin/pro-con      # claude は起動しない。模擬の backend (fake) が状態を進める
```

- 1 秒ごとに `Poll` し、fake の模擬時間が 1 分進む (カードが列を進む・PG が質問する・watchdog が停滞を拾う・リソースの順番が回る)
- attach (`a`) は本物の `claude attach` の代わりに `pro-con fake-attach <id>` を `tea.ExecProcess` で起動する。
  画面の明け渡しと戻りは本番と同じ経路を通る

| キー | 動作 |
|---|---|
| ←→↑↓ / hjkl | カードの選択 |
| enter / esc | 詳細の開閉 |
| a | PG の session を開く (今は模擬) |
| r | 質問待ちのカードに回答する |
| o | 追加オーダー (tab で 追記 / 方針変更 / 別件) |
| b | btw (PG を止めずに状況を聞く) |
| q | 終了 |

## 構成

| package | 役割 |
|---|---|
| `card` | ドメイン (カード・状態・不変条件の検査)。UI にも backend にも依存しない |
| `backend` | UI と状態の持ち主の境界 (`Backend` interface / `Command` / `Snapshot`)。UI はここより下を知らない |
| `fake` | 模擬 backend。本物に差し替えるときは `backend.Backend` を満たす実装を足し、`main.go` の 1 行を替える |
| `ui` | bubbletea v2 の TUI。状態は持たない (Snapshot を描き、Command を送るだけ) |

- `fake` の dispatcher / watchdog / リソース列は**模擬**で、本番の判定ではない。本番の判定を育てるなら fake から切り出す
- 🚨 **見た目 (列・語・記号) は未確定**。issue 415 の段階 1 で見てもらって決める。それまで表示の文言にはテストを書かない
  (テストはキー入力 → Command → 状態遷移のつなぎ込みだけ)
- 列幅の下限は 14 桁なので、**幅 89 桁未満の端末では行が端末幅を溢れる** (見た目を決めるときに列の畳み方と一緒に決める)
- `src/tuikit` は使っていない (ハリボテの段階で依存を増やさない)。見た目が決まって本体を組むときに寄せる
