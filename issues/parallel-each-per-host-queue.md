# parallel-each: ドメイン別の並列数制御

起票日: 2026-04-23

入力が URL の場合、現状の global `-P` に加えて **ホスト単位で並列数の上限** を設定できるようにしたい。実装は地味に重いので、着手前に決め事をここにまとめる。

## 背景 / モチベーション

- 今は `-P 4` のような単一の並列度しかない
- 「特定ホストには 1 並列、他は 4 並列」ができず、結果として一番弱いホストに合わせて global P を絞る羽目になる
- aria2 の `--max-connection-per-server` や httpie/wget の `--wait` などで各自工夫している領域
- bulk download 系で特によく効く（複数ドメインから落とす時、ホスト間は独立、ホスト内は過剰アクセスを避けたい）

## UI 案

### フラグ

```
--per-host N                   # 全ホストに一律 N
--per-host host=N,host2=M,...  # ホスト個別指定
```

両方同時指定時は個別指定が勝つ。指定のないホストは `--per-host N`（`N` が指定なら）または global `-P` にフォールバック。

### 入力が URL でない場合

- URL として parse できない行: global pool 扱い（今の `-P` だけが効く）
- `localhost` / IP アドレス: hostname をそのままキーに使う
- scheme / port 違いはドメイン単位で **統合** する（`https://x.com` と `http://x.com:8080` は同じバケット）

### 「ホスト」の粒度

- `url.Parse(line).Hostname()` の結果をキーに使う
- eTLD+1 に丸める案（`www.x.com` と `api.x.com` を同バケット）は見送り → 明示的に別ホスト扱い。ユーザーが欲しければ両方に同じ limit を設定するだけで済む

## アーキテクチャ案

### 案 1: runOne の冒頭でドメイン semaphore を acquire（簡易）

```go
// 擬似コード
sem := r.domainSem(domain)
sem.Acquire()
defer sem.Release()
// exec
```

**メリット**: 実装量が小さい。今の single queue 構造を変えなくていい。

**デメリット**: head-of-line blocking。worker がドメイン満杯の item を握ったまま semaphore を待つので、別ドメインの item が queue に残っていても処理されない。P 個の worker がすべて同じ（満杯）ドメインの item を握って詰まると事実上停止する。

**回避**: 入力がドメインバラバラなら実用上問題ない。同ドメインが連続する入力（urls.txt を domain でソートしていたりする）では致命的。

### 案 2: ドメインごとに sub-queue + capacity-aware dispatch（本格）

```
r.subQueues: map[domain]*subQueue{items []runnerJob, inflight int, limit int}
r.globalQueue: 未分類 item
dispatcher: 「どこかに空きドメイン + item がある」なら item を選んで worker へ
```

**メリット**: UX 良好。head-of-line blocking なし。ドメイン粒度で動的に dispatch。

**デメリット**:
- 今の `r.queue` 単体スライス + peek/commit パターンを書き換え
- `PendingSnapshot` (TUI queue view) の順序が自明じゃなくなる（ドメインごとにグルーピング表示 or フラット表示+ドメインラベル？）
- `Enqueue` / `EnqueueFront` の「先頭に入れる」が複数 sub-queue 間で意味が薄れる → ドメインの sub-queue 内での先頭、とするのが妥当
- リトライ時の再投入もドメインごと

### 推奨

**案 2 + オプトイン**で着手。`--per-host` フラグを指定した時だけ sub-queue 化するロジックを有効にし、指定なし時は今の single-queue コードパスをそのまま通す。こうすれば既存挙動は完全に温存できる。

## 気になる小さい決めごと

- `urls.txt` の同じ行に URL じゃないものが混ざったら: global pool に投げて `-P` に従わせる
- dry-run (`-n`) 時の表示: `[domain] cmd` のようにドメインを先頭につける？ そこまでは要らない気もする
- TUI の queue view: ドメインごとの残件数をヘッダに出すと欲しくなりそう (`queue — 50 items  [a.com: 30, b.com: 20]`)
- TUI の active slots: 現在実行中の worker のドメインも表示したい
- result.log のフォーマット変更: **不要**。ドメインは URL から parse できるので追加列は入れない
- `--per-host 0`: そのホストは無制限（global `-P` の範囲で動く）として扱う
- `--per-host 1` だけ指定した場合の挙動: 全ホスト 1 並列（= 実質シリアル per host）

## 見積もり

- 案 1 (簡易): +150 行程度、1 時間
- 案 2 (本格): +400–600 行、1 日程度。Enqueue/EnqueueFront/PendingSnapshot の書き換え、TUI の queue view 調整、テスト追加

## TODO（実装時）

- [ ] URL parser の挙動確認（IPv6、percent-encoded host、port あり・なし）
- [ ] ドメインを取り出すヘルパ + ユニットテスト
- [ ] sub-queue 構造の実装（map[domain]*subQueue、global fallback queue）
- [ ] dispatcher を capacity-aware に書き換え
- [ ] Enqueue / EnqueueFront を sub-queue 対応に
- [ ] TUI queue view のグルーピング表示
- [ ] TUI active slots にドメインを表示
- [ ] flag 解析（`--per-host` の一括形式 + `host=N,...` 形式のパース）
- [ ] help 更新
- [ ] 動作確認: 単一ドメイン / 複数ドメイン / 非 URL 混在 / `--per-host 0`
