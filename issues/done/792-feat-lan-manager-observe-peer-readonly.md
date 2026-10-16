# 792 (feat): lan-manager の段 4 のうち、相手の設定を ssh で読むだけの観測を作る

起票日: 2026-10-16

> **着手の条件 (trigger)**: issue 789 (human。固定 IP・画面共有・切断後も走るかを手で確かめる) が済んだら。
> 読むだけでも本物の `networksetup` を相手の Mac で起こすので、789 で相手の環境を一度見てから作る方が安全と判断した (2026-10-16)。
> 789 の 1 で取る `Manual Configuration` の本物の出力は、ここのテストの fixture にも使う。

## 概要

issue 747 の段 4 は、相手の Mac の有線を固定 IP にし、鍵を配る。その前に、相手の今の状態を読む部分 (setup.Observed の Peer) がまだ無い。
今は `setup.Plan` は観測を受け取るだけで、組み立てる本番の呼び出し元が無い (747 の監査で確認)。

`setup.PeerObs` の欄と、どこから埋めるか (反証レビューの指摘で、ssh だけでは埋まらない欄を分けた):

| 欄 | 出どころ |
|---|---|
| Host / WifiIP / LanIP / SSHOpen | Bonjour (discover。今の一覧の行が既に持つ) |
| Managed / LedgerLAN | 台帳 |
| KeyOK | `ssh -o BatchMode=yes <相手> true` |
| NetKnown / Service / IP / Method | ssh で相手の `netinfo.Read` を走らせる (下) |

これを組んで `setup.Plan` に渡し、詳細カード (または初期設定の画面) に「次にやること」を出す。

## 対応方針

- 読むだけ。設定を変えるコマンドは打たない (変えるのは 747 の段 4 の本体)
- 相手の読み取りは `netinfo.Read` を、ssh で包んだ `netinfo.Runner` で走らせる (ParseServiceOrder / ParseGetInfo を個別に呼ばない)。
  サービス名は番号の行・無効のサービスは数えない・有線は I/F ごとの分類 (`WiredIF`) で決める、をそのまま引き継ぐ。Service はルーターの無い有線の
  サービス (`Offline()` の先頭) にする
- ssh の宛先: 台帳に載っている相手は台帳の検証済みの値 (`ledger.ValidUser` / `ValidLAN`)。初期設定の対象 (`Managed=false`。台帳に user も内部 IP も無い) は、
  Bonjour の Wi-Fi の IP と、この Mac の `$USER` (今の s と同じ推測) で組む。どちらの場合も宛先の前に `--` を置く (747 の第 3 波)
- KeyOK が偽 (BatchMode の ssh が通らない) なら、NetKnown も読まない (読めない)。`Plan` は鍵の手順で先に止まるので整合する。
  ssh が timeout・接続の失敗で終わったときは「鍵が無い」ではなく「判定不能」として画面に出す (KeyOK=false に丸めない)
- 応えない相手で止まらないよう、上限つきで起こす (ssh の `-o ConnectTimeout=5` と、`context.WithTimeout` の全体 15 秒。値は着手のときに実測して決め直す)
- テストは偽の ssh (Runner の差し替え) で書く。本物の相手には 789 の後に 1 回だけ当てる

## 関連ファイル

- `src/mac-lan-manager/setup/plan.go` (`Observed` / `PeerObs` / `Plan`)
- `src/mac-lan-manager/netinfo/netinfo.go` (読み取り)
- `src/mac-lan-manager/lanmgr/` (表示)
- issue 747 / 789

## 進捗

- [ ] 反証レビュー
- [ ] 着手 (789 の後)

## 決着 (2026-10-16)

着手の条件 (789 の後) を待たず、747 の段 4 の配線の中で作った (ユーザーの指示「段 4 をやって」)。対応方針の項目ごとの在り処:

- 鍵の確認と相手の `netinfo.Read` を ssh で包む: `src/mac-lan-manager/setup/remote.go` の `ObservePeer` (宛先は `Dest`。`--` と HostKeyAlias を付け、Validate で検査)
- timeout・接続の失敗を「鍵が無い」に丸めない: `ObservePeer` は stderr の `Permission denied (` の 255 だけを「鍵で入れない」とし、他は `ErrUnknown`
- 上限: ssh 1 本ごとに `ConnectTimeout=5`、全体は `live.ObserveTimeout` (15 秒)。本物の ssh は `src/mac-lan-manager/live/ssh.go` の `SSHRun` / `Observe`
  (dotfiles-41 の commit を 2026-10-16 に取り込んだ)。`mac-lan-manager dump --peer <IP>` でも同じ観測を出す
- Plan に渡して「次にやること」を出す: 初期設定の画面 (`lanmgr/setup.go`・`setupview.go`。一覧の行で Enter)
- テストは偽の ssh (`setup/remote_test.go`・`live/ssh_test.go`)

残り: `Manual Configuration` の本物の出力の fixture と、本物の相手に 1 回当てるのは issue 789 (最初に使うときが兼ねる)。
