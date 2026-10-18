# 832 (human): lan-manager の通信速度の測定を本物の Mac 同士で確かめる

起票日: 2026-10-18
期限: 2026-10-25

## 概要

issue 827 で入れた通信速度の測定 (詳細カードで `t` → 確認の板で `y`) を、本物の相手で 1 回動かして値と後始末を確かめる。
偽物の ssh と本物のパイプではテストしたが、本物の ssh・相手の `wc -c` / `head -c`・接続の使い回し (ControlMaster) を通した確認は機械ではできない (帯域を使い切るので、人の判断で流す)。
反証レビューは省略 (手順だけの human issue)。

## 手順

1. `GO_AUTOBUILD_SYNC=1 bin/mac-lan-manager` を開き、管理対象のタブで有線で繋いだ相手 (例: m2macmini) を選ぶ
2. 詳細カードに `通信速度  未測定  t 測る` が出ていることを見て、`t` → 板の宛先が内部 IP と `(有線)` であることを見て `y`
3. 約 10 秒後に `↑ N  ↓ N (有線)  HH:MM に測定 (IP)` が出る。Thunderbolt なら数百 Mbps 以上、途中に 100M の機器があれば 90 前後と `🚨 100 Mbps 付近で頭打ち` が出るはず
4. 測定中に `q` で閉じ、相手で `ps ax | grep -E 'wc -c|head -c'` に残りが無いこと、この Mac で `ps ax | grep 'ssh.*wc -c\|ssh.*head -c'` が空であることを見る
5. `~/Library/Logs/mac-lan-manager.log` に `speed start` / `speed done` (閉じたなら `speed cancel`) が出ていることを見る

## 期待と違ったら

- 値が明らかにおかしい・残りのプロセスがある → issue 827 を open に戻して (done から出して) 症状と log の行を書く
- 鍵で入れない相手で `鍵で入れないので測れない` 以外の文が出る → 同じく 827 へ
