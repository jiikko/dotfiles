# 964 (bug): lan-manager の有線の名前の済み (wired_ok・一覧の !) が、名前を引けるだけで届かない相手を「済み」と読む

起票日: 2026-10-29

## 概要

847 の起点の実例の切り出し。有線の名前が「済み」かを、名前の解決 (名前が台帳の内部 IP だけに引けるか) だけで判定している。
相手の有線の I/F が替わって内部 IP を持たなくなっても、広告する LaunchAgent が古い IP を広告し続けるので、判定は「済み」のまま変わらない。

## 詳細 (実例 2026-10-20。847 の「なぜ」節)

- m2macmini の有線を、備え付けの LAN から USB-C のアダプタへ替えた。新しいサービスは DHCP で 169.254.48.172 になった
  (固定 IP はサービスごとの設定なので、前のサービスにしか付いていない)
- 広告する LaunchAgent は `m2macmini.wired.local → 192.168.100.12` を広告し続けた。台帳の内部 IP も 192.168.100.12 なので、
  `lanmgr/wiredname.go` の `wiredCheck` は wiredOK を返した。air5 からは 192.168.100.12 に ping 0/3・22 番 timeout (847 の実機の結果)
- その結果、次の 4 か所が届かない相手を「済み」と出す (同じ wiredCheck の結果を読む)
  - `dump --json` の `wired_ok: true` (`lanmgr/report.go` の WiredOK)
  - `dump` の人向けの出力の「有線の名前: … ✓ 引ける」(`dump.go` の WiredName の行)
  - 一覧の初期設定の印 `!` が付かない (`setupPending` は wiredMissing のときだけ「途中」と読む)
  - 繋ぐ・コピー・画面共有の宛先 (`target`) が、届かない有線の名前になる (wiredState が ok のとき名前を渡すため)
- 847 で足した一覧の「疎通」の列は、同じ状態を `W✗` と出す (`lanmgr/reach.go` の reachCell)。だから画面では、`W✗` が出ているのに `!` は付かず、宛先は名前になる、という食い違った表示になる。
  dump は疎通を確かめない (Passive) ので、CLI では「✓ 引ける」しか出ない
- 2026-10-20 の後、m2macmini は初期設定をやり直して 192.168.100.15 で届いている (847 の進捗)。今の実機でこの状態になっているとは限らない

## 対応方針 (未決。判断が要る)

847 で「`wired_ok` / `!` の判定に疎通を混ぜない」と決めた理由は 2 つ: 判定の意味が変わること、名前を引くたびに ping を打つことになること。
候補:
1. 初期設定の観測 (相手の有線のサービスが manual で、内部 IP を今持っているか) と突き合わせる。ssh が要るので、一覧を開くたびには読めない
2. 847 の自動の確認 (reach の結果) を `!`・`wired_ok`・`target` の判定材料に足す。次の場合の扱いを決める必要がある: reach の結果が無い間 /
   結果が古い・確かめた名前の組が今と違う (`reachResult` の at・asked) / reach が動かない経路 (dump などの Passive、`Options.Reach` が無い画面)
3. 判定は変えず、wiredStatus に「引けるが届かない」の状態を足し、`!`・`wired_ok`・`target` がそれぞれその状態をどう読むかを個別に決める
4. 根の側で止める: 広告する側の LaunchAgent が、広告する IP を持つ I/F が無くなったら広告をやめる (古い広告がそもそも残らない。
   ただし広告をやめても、問い合わせた側のキャッシュに約 4 分残る。setup/wiredname.go の冒頭のコメントの実測)

## 関連ファイル

- `src/mac-lan-manager/lanmgr/wiredname.go` (`wiredCheck` / `wiredState` / `setupPending` / `target`)
- `src/mac-lan-manager/lanmgr/report.go` (WiredOK)、`src/mac-lan-manager/dump.go` (人向けの出力)
- `src/mac-lan-manager/lanmgr/reach.go` (847 の自動の確認)

## 進捗

- [x] 起票時の反証レビュー (sonnet、読み取りのみ。2026-10-29): 核の断定 (wiredCheck は解決だけを見る・台帳の内部 IP が行の lanIP になる・
  `!` / target / WiredOK の読み方・実例の数値) は反証されなかった。反映: dump の人向けの出力を影響箇所に足す / 疎通の列の W✗ と併存する
  ことを書く / 方針 2 で決めることを足す / 方針 3・4 を足す
- [ ] 方針を決める
