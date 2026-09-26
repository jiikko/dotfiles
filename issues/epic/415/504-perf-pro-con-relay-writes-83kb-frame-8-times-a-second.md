# 504 (perf): 画面の中継が、見る人がいなくても 83KB のフレームを毎秒約 8 回書き直す

起票日: 2026-09-26

## 概要

画面の中継 (`relay/relay.go` の `Writer.loop`。443 で導入) は、描き直しの中身が変わるたびに最短 100ms ごとに `<id>.frame` を書き直す。
「中身が同じ描き直しは書かない」(`sameContent`) で抑えるつもりだったが、作業中の PG が居るとスピナー (`ui/spinner.go` の `spinInterval` = 100ms) が
毎回画面の中身を変えるので、その抑えが効かない。読むのは `pro-con screen` / `pro-con ps` を人やエージェントが叩いたときだけ。

## 実測 (2026-09-26、作業中の PG 1 体・画面 1 つ)

| 何を | 値 |
|---|---|
| 5 秒間に書き直された回数 (`stat -f %Fm` を 20ms ごとに見た) | 42 回 (約 8 回/秒) |
| 1 枚の大きさ | 83 KB (`ansi` 40 KB + `plain` 21 KB を JSON に) |
| 1 回の書き出し (`ansi.Strip` + `json.Marshal` + `writeAtomic`、本物の 1 枚で benchmark) | 0.53 ms / 204 KB / 33 allocs |
| 合計 | 毎秒 約 4 ms の CPU・1.6 MB の確保・700 KB の書き込み (一時ファイルの作成 + rename が毎秒 8 回) |

CPU としては小さいが、画面の描画と同じプロセスで確保する (494 の GC の負荷に足される) のと、ディスクへ書き続ける。

## 対応方針 (案)

- 読む側が居るときだけ書く (読む側が要求の印を置く / 読み出しの最後の時刻を見る)、または中身が変わっても書く間隔を 1 秒に延ばす。
  🚨 443 の「最新の 1 枚を読める」の約束 (`pro-con screen` が今の画面を見る。e2e とエージェントの確認に使う) を崩さない形を選ぶ
- どちらにしても、効果は「5 秒間の書き直しの数」で before / after を測る

## 関連ファイル

- `src/pro-con/relay/relay.go` — `Writer.Put` / `loop` / `sameContent` / `minInterval`
- `src/pro-con/main.go` — `openRelay` (受け口の配線)
- `src/pro-con/screencmd.go` — 読む側

## 進捗

- [x] 実測 (上の表)
- [x] 反証レビュー (読み取り専用のサブエージェント 1 体): 反証されず
- [ ] 対応方針の形を決める
