# 830 (risk): lan-manager の走査: 1 つの I/F に相手の IP が 2 つ見えると片方が消える (未実測)

起票日: 2026-10-18

## 概要

issue 826 の 3 を切り出したもの。`src/mac-lan-manager/discover/scan.go` の gatherOnce は `p.Addrs[a.If] = a.IP` で、鍵がこの Mac の I/F の番号だけなので、
相手の Wi-Fi と有線が同じルーターの網に居て en0 に 2 本の A が届くと、後に来た方だけが残る (応答の順で入れ替わりうる)。

## 現状 (2026-10-18 に air5 で実測)

`dns-sd -G v4 ms.local` / `dns-sd -G v4 m2macmini.local` は I/F ごとに 1 行ずつ (en0 = 192.168.50.x / en5 = 192.168.100.x)。
相手の 2 本の I/F が同じ網に居る構成がこの Mac の周りに無く、起きるかを確かめられていない。推測で Addrs の形 (map[int]string) を変えない。

## 再開の trigger

相手の 2 本の I/F が同じ網に居る構成で、`dns-sd -G v4 <host>` が同じ I/F の番号で 2 行 (別の IP) を返すのを見たとき。
そのときは stdout を残し、`discover/testdata` に本物の出力として置いてから直す (`src/mac-lan-manager/README.md` の「出力を読む処理の正本は testdata」)。

反証レビューは通していない (記録だけの risk。直すときに通す)。
