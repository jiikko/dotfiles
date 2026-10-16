# 792 (feat): lan-manager の段 4 のうち、相手の設定を ssh で読むだけの観測を作る

起票日: 2026-10-16

> **着手の条件 (trigger)**: issue 789 (human。固定 IP・画面共有・切断後も走るかを手で確かめる) が済んだら。
> 読むだけでも本物の `networksetup` を相手の Mac で起こすので、789 で相手の環境を一度見てから作る方が安全と判断した (2026-10-16)。
> 789 の 1 で取る `Manual Configuration` の本物の出力は、ここのテストの fixture にも使う。

## 概要

issue 747 の段 4 は、相手の Mac の有線を固定 IP にし、鍵を配る。その前に、相手の今の状態を読む部分 (setup.Observed の Peer) がまだ無い。
今は `setup.Plan` は観測を受け取るだけで、組み立てる本番の呼び出し元が無い (747 の監査で確認)。

- 相手の有線のサービスの方式と IP (`ssh <相手> networksetup -listnetworkserviceorder` と `-getinfo <サービス>`)
- 鍵で入れるか (`ssh -o BatchMode=yes <相手> true`)

これらを読み、`setup.Observed.Peer` を組んで、詳細カード (または初期設定の画面) に「次にやること」を出す。

## 対応方針

- 読むだけ。設定を変えるコマンドは打たない (変えるのは 747 の段 4 の本体)
- 相手の出力の読み取りは netinfo の ParseServiceOrder / ParseGetInfo をそのまま使う (サービス名は番号の行・無効のサービスは数えない、を引き継ぐ)
- ssh の宛先は台帳の検証済みの値だけ (`ledger.ValidUser` / `ValidLAN`) で組み、宛先の前に `--` を置く (747 の第 3 波)
- 応えない相手で止まらないよう、上限つきで起こす (`ConnectTimeout` と全体の上限)
- テストは偽の ssh (Runner の差し替え) で書く。本物の相手には 789 の後に 1 回だけ当てる

## 関連ファイル

- `src/mac-lan-manager/setup/plan.go` (`Observed` / `PeerObs` / `Plan`)
- `src/mac-lan-manager/netinfo/netinfo.go` (読み取り)
- `src/mac-lan-manager/lanmgr/` (表示)
- issue 747 / 789

## 進捗

- [ ] 反証レビュー
- [ ] 着手 (789 の後)
