# 767 (refactor): pro-con の一時ファイル + rename の 5 か所を atomicfile に寄せる (固定名 .tmp の取り合いも消す)

起票日: 2026-10-15

## 概要

pro-con は「一時ファイルに書いて rename する」書き込みを 5 か所で手書きしている。`src/atomicfile` (`atomicfile.Write`) が同じ働きを持ち、
pro-con の go.mod にも既に (indirect で) 入っている。寄せれば同じ手順の実装が 1 つになり、下の固定名の取り合いも構造で消える。

## 詳細 (2026-10-15 に grep と本文で確認)

| 場所 | 一時ファイルの名前 | 気になる点 |
|---|---|---|
| `pro-con/store/store.go` の `writeAtomic` | `.<base>.tmp-*` (CreateTemp) | — |
| `pro-con/relay/relay.go` の `writeAtomic` | `.tmp-*` (CreateTemp) | `relay.sweep` が `.tmp-*` を残骸として消している |
| `pro-con/live/registry.go` の `writeRows` | `<base>.tmp-*` (CreateTemp) | — |
| `pro-con/upgrade/resume.go` の `Save` | **`path + ".tmp"` (固定名)** | 同時に 2 本が書くと同じ一時ファイルを取り合う (doctor の issue 219 と同じ種類) |
| `pro-con/dispatcher/e2e.go` の `(E2E).save` | **`sessionsPath() + ".tmp"` (固定名)** | 同上。rename に失敗したとき一時ファイルを消さない |

- 固定名の 2 か所で、実際に 2 本が同時に書く経路があるかは未確認 (取り合いが起きうる形であることまでを確認した)
- 参考: `pro-con/store/attach.go` の stage も先頭の `.` を手掛かりに残骸を掃除している

## 対応方針

1. 5 か所を `atomicfile.Write` (または同等の API) に置き換える。fsync・パーミッション・失敗時の後始末の差を、置き換えの前に表にする
   (`_claude/rules/survey-receiver-guards-before-passing-new-values.md` の「N 個の手組みを 1 つの道具に寄せる」)
2. 🚨 **残骸の掃除と名前の規則を同じ変更で合わせる**: atomicfile の一時ファイル名 (`<base>.tmp.*`) は relay の `sweep` (`.tmp-*`) と
   store/attach の掃除 (先頭の `.`) の手掛かりと違う。置き換えると、落ちた書き込みの残骸を掃除が拾わなくなる
3. 固定名の 2 か所は、取り合いを再現するテスト (2 本を同時に書かせる) を先に置いて red を見てから直す

## 関連ファイル

- `src/atomicfile/` / 上の表の 5 ファイル / `pro-con/store/attach.go`
- 調査の全体: 773
