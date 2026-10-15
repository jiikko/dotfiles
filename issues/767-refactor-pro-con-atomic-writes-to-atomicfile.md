# 767 (refactor): pro-con の一時ファイル + rename の 5 か所を atomicfile に寄せる (失敗したときの一時ファイルの残りも消す)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-15〜）

起票日: 2026-10-15

## 概要

pro-con は「一時ファイルに書いて rename する」書き込みを 5 か所で手書きしている。`src/atomicfile` の `atomicfile.Write(path, data, perm)` が同じ働きを持ち、
pro-con の go.mod にも既に (indirect + replace で) 入っている。寄せれば同じ手順の実装が 1 つになり、下の「失敗したときに一時ファイルが残る」も構造で消える。

## 詳細 (2026-10-15 に grep と本文で確認)

| 場所 | 一時ファイルの名前 | 失敗したときの後始末 |
|---|---|---|
| `pro-con/store/store.go` の `writeAtomic` | `.<base>.tmp-*` (CreateTemp) | 消す |
| `pro-con/relay/relay.go` の `writeAtomic` | `.tmp-*` (CreateTemp) | 消す。`relay.sweep` が `.tmp-*` を残骸として消している (768 の印の作りかけ `.tmp-<id>` と同じ接頭辞) |
| `pro-con/live/registry.go` の `writeRows` | `<base>.tmp-*` (CreateTemp) | 消す |
| `pro-con/upgrade/resume.go` の `Save` | `path + ".tmp"` (固定名) | **書き込みの失敗では消さない** (rename の失敗だけ消す) |
| `pro-con/dispatcher/e2e.go` の `(E2E).save` | `sessionsPath() + ".tmp"` (固定名) | **どの失敗でも消さない** |

- 固定名でも、取り合いは起きない: e2e は書き手が dispatcher だけで `e2eMu` で並べている。resume の path は `resume-<pid>-<nanotime>.json` でプロセスごとに別。
  問題は名前ではなく、失敗した書きかけが残ること (glogx の `doctor_cache.go` を直した issue 219 の (a) と同じ形)
- fsync はどの 5 か所も呼んでおらず、`atomicfile.Write` も呼ばない (寄せても差は出ない)
- 寄せない 6 か所目: `pro-con/store/attach.go` の `copyFile` も CreateTemp + rename だが、io.Copy で流しながら大きさの上限を見るので、`[]byte` を受ける `atomicfile.Write` に合わない

## 対応方針

1. 5 か所を `atomicfile.Write` に置き換える。パーミッション (CreateTemp の 0600 に頼っている箇所がある。relay のコメント) と失敗時の後始末の差を、置き換えの前に表にする
   (`_claude/rules/survey-receiver-guards-before-passing-new-values.md` の「N 個の手組みを 1 つの道具に寄せる」)
2. 🚨 **残骸の掃除と名前の規則を同じ変更で合わせる**: atomicfile の一時ファイル名 (`<base>.tmp.*`) は relay の `sweep` (`.tmp-*`) と store/attach の掃除 (先頭の `.`) の手掛かりと違う。
   置き換えると、落ちた書き込みの残骸を掃除が拾わなくなる。relay の `sweep` は 768 (印の共有) とも重なるので、**先に入れる方がもう片方の前提を変える**。768 と順番を決めてから着手する
3. 失敗したときに一時ファイルが残らないことを、書き込みを失敗させるテスト (書けない置き場) で red を見てから直す

## 関連ファイル

- `src/atomicfile/` / 上の表の 5 ファイル / `pro-con/store/attach.go`
- 768 (relay の `sweep` を共有する) / issue 219 (glogx の doctor_cache の一時ファイル) / 調査の全体: 773

## 反証レビュー

- 2026-10-15 (read-only のサブエージェント 1 体): P2 固定名の取り合いは経路が無い (e2eMu・resume の path は pid + nanotime) → 「失敗したときに残る」に立て直し、タイトルも直した /
  P2 768 と relay の `sweep` を取り合う → 対応方針に書いた / P3 attach の `copyFile` が 6 か所目 → 寄せない理由つきで書いた / P3 fsync の差が無いことを書いた。
  219 を「doctor の」と書いていたのは glogx の `doctor_cache.go` の誤り → 直した
