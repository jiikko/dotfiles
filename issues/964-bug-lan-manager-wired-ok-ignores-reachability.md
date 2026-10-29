# 964 (bug): lan-manager の有線の名前の済み (wired_ok・一覧の !) が、名前を引けるだけで届かない相手を「済み」と読む

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

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

## 対応方針 (決定: 4。ユーザー選定 2026-10-29)

4 を採った (判定の意味は「名前が引ける」のまま変えず、古い広告そのものを残さない)。以下は選ぶ前の候補の記録。

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
- `src/mac-lan-manager/setup/wiredname.go` (advertLoop / args / AdvertTag / StatusScript。採った方針 4 の実装)

## 進捗

- [x] 起票時の反証レビュー (sonnet、読み取りのみ。2026-10-29): 核の断定 (wiredCheck は解決だけを見る・台帳の内部 IP が行の lanIP になる・
  `!` / target / WiredOK の読み方・実例の数値) は反証されなかった。反映: dump の人向けの出力を影響箇所に足す / 疎通の列の W✗ と併存する
  ことを書く / 方針 2 で決めることを足す / 方針 3・4 を足す
- [x] 方針を決める (4。ユーザー選定 2026-10-29)
- [x] 広告をループにする — 「fix(mac-lan-manager): 有線の名前の広告を、その IP を I/F が持っている間だけにする (issue 964)」。
  LaunchAgent が sh のループを起こし、10 秒ごとに ifconfig で IP を見て、持っている間だけ dns-sd -P を子として走らせる
  (落ちたら起こし直す・TERM では子も止める)。launchctl print の arguments の 7・8 番目 (名前・IP) は前の形と同じ位置で、4 番目を形の
  目印 (AdvertTag) にした。前の形で走っている Mac は済みにしない (初期設定の名前の手順で入れ直させる)
  - 実測 (macOS 27.0.1): `launchctl submit` した sh -c のループの job で、arguments が 1 行ずつ、4・7・8 番目に目印・名前・IP が並ぶ。
    remove で job の子 (sleep) まで止まった。本物の ifconfig / dns-sd でループを手で回し、持っている IP (192.168.50.250) の名前は
    dscacheutil で引け、持っていない IP (192.0.2.1) の名前は引けず、TERM で子の dns-sd が止まり何も残らなかった
  - 変異で red: IP を失ったときの停止を消す / TERM の trap の子の kill を消す / Advertises の目印の比較を消す / ifconfig の照合の
    IP の後ろの空白を消す (.11 が .111 に一致。最初は 3 周の待ちで見逃し、20 周に広げて red)
- [x] 敵対的レビュー (opus、読み取りのみ) 1 周目 — 「test(mac-lan-manager): 広告のループを plist から読み戻した argv で回し、確認の板の説明を今の形にする」。
  直した: テストがループの argv を手で組んでいて、args の並び・plist の実体参照の往復を見ていなかった (plist から読み戻す形にし、
  インスタンス名とサービスの入れ替え・二重エスケープの変異で red を確認) / 確認の板の「dns-sd -P を起こし続ける」。
  受けた (直していない。再提起には実測を添える):
  - 済みの意味が「dns-sd が走っている」から「ループが走っている」に変わった: ループが IP を持たずに待っている間も名前の手順は済み。
    前の形 (届かない IP を広告して済み) より悪くはならない。手動の IP を持つサービスの I/F が抜けた間に済みと読む形は推測で、実例は無い
  - pid の使い回し: dns-sd が自分で落ち、10 秒以内に同じ pid が別のプロセスに振られると、起こし直さず、IP を失ったときにそのプロセスに
    TERM を送る。macOS は pid を順に振るので確率は低い (推測。実例は無い)
- [x] 敵対的レビュー 2 周目 (opus、直した差分): 壊せなかった。P3 2 件 (板の「10 秒」が手の写し・板の 1 行目と 2 行目の言い回しの食い違い) を
  「refactor(mac-lan-manager): 広告のループの周期を setup.AdvertEvery にし、確認の板の言い回しを揃える」で直した
- [x] 既に入っている Mac (前の形で走っている) の入れ直し: 自動では起きない (一覧の `!` は名前の解決だけを見るので、前の形でも付かない)。
  各 Mac で初期設定の名前の手順を実行し直す人の作業として [967](967-human-reinstall-wired-name-advert-on-each-mac.md) に切り出した
