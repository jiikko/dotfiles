# 861 (bug): ルーターの無い Wi-Fi で見えた相手を有線 (内部 IP) と数える

起票日: 2026-10-22

## 概要

lan-manager の全体監査 (865) の P2 (encapsulation E1)。「内部の網に使う有線の I/F か」は `!s.NotWiredLAN() && s.WiredIF(dev)` で決まる
(Hardware Port で無線を除き、ルーターの無い I/F を内部とする)。この組み立てを呼び出し側が手で書いていて、5 か所のうち 2 か所で無線の除外が漏れている。

既出: done の 747 の「残っているもの」に「記録だけ (直していない・未確認)」として、done の 828 に P2 の既知として載っている
(trigger は「ルーターの無い Wi-Fi で lan-manager を開いたとき」で、まだ来ていない)。issue として起こすのは、反証レビューで
**宛先の選び方と断りの理由にまで効く** (下の「何が壊れるか」の 2 と 3) ことが分かり、記録だけの扱いより重いと判断したため。

## 詳細

- `netinfo.Snapshot.WiredIF` / `class` は Hardware Port を見ない。ルーターが無ければ Wi-Fi も `ifWired` になり、`Kind` は KindWired / KindLinkLocal を返す
- 無線を除いている (3 か所): `netinfo.Offline`、`lanmgr/settings.go:lanIFs`、`lanmgr/setup.go:selfWired`
- 除いていない (2 か所):
  - `lanmgr/model.go:rows` の `wiredService := k == netinfo.KindWired || (k == netinfo.KindLinkLocal && m.self.WiredIF(dev))`
  - `selfLan` の I/F で決める側。ただし `pf.LanIF` を設定する経路は `settings.go:lanIFs` の候補 (無線を除く) だけなので、届くのは prefs を手で書いたときだけ。実害は `rows` 側
- 何が壊れるか:
  1. この Mac の Wi-Fi に DHCP が取れず 169.254 になったとき (またはルーターの無い Wi-Fi)、同じ Wi-Fi で見えた相手の IP が「内部 IP」になり
     `r.wired=true` になる。設定の「有線で見えた相手だけ」を通る。この Mac 側の内部の網 (Offline / selfWired) は Wi-Fi を除くので、画面と初期設定で答えが割れる。見えるのは `(無線)` の札だけ
  2. この Mac がルーターのある有線とルーターの無い Wi-Fi の両方に居ると、相手は有線で `KindInternet` (mainIP)、Wi-Fi で `KindWired` (lanIP) に見える。
     `connectIP` は lanIP を優先するので、s と speedtest は有線ではなく Wi-Fi の IP へ繋ぐ
  3. この Mac の内部 IP が帯の中に固定されていて、相手が Wi-Fi の 169.254 にしか見えないと、`lanAway` と `mainIP == ""` が重なり、s は「有線で網が分かれている」という見当違いの理由で断る
- 発火条件: Wi-Fi に DHCP が取れない状態か、ルーターの無い Wi-Fi に繋いだ状態 (実機では未再現。コードの読みで確認)
- `TestLinkIsIndependentOfRouter` は、ルーターの無い Wi-Fi の Kind が Internet でないことしか見ていない

## 対応方針

- 持ち主の側に `netinfo.Snapshot.LanIF(dev) bool` (Port の除外 + ifWired) を作り、5 か所をこれに寄せる。`rows` の wiredService もこれを通す
- ルーターの無い Wi-Fi の上で見えた相手が `wired` にならないテストを足す

## 関連ファイル

- `src/mac-lan-manager/netinfo/netinfo.go` (`WiredIF` / `class` / `NotWiredLAN` / `Offline`)
- `src/mac-lan-manager/lanmgr/model.go` (`rows`)、`lanmgr/settings.go` (`lanIFs`)、`lanmgr/setup.go` (`selfWired` / `selfLan`)

## 進捗

- [ ] `LanIF` に寄せる修正とテスト
