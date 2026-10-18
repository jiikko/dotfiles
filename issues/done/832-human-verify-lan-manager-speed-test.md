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

## 結果 (2026-10-18)

- ユーザーが手順を実行: 管理対象の ms で `t` → `y`、表示は `80 / 80` 前後 (ユーザー報告「80/80って出た」)
- 操作のログ (`~/Library/Logs/mac-lan-manager.log`) を main が読んで確かめた: `speed start ms.local ip=192.168.100.13 link=有線` / `speed done ms.local up=88395211 down=79951121`
  (bit/s。↑ 88 Mbps ↓ 80 Mbps)。この Mac に `ssh … wc -c` / `head -c` の残り無し (`ps ax`)。相手側の残りの確認・測定中の `q` は報告に無い (未確認)
- 同じ日に CLI (`mac-lan-manager speedtest ms`) で 2 回測り直した: ↑ 82〜83 Mbps ↓ 80 Mbps、有線、`capped_100m=true`
- **読み**: 有線 (Thunderbolt / USB LAN) なのに 100 Mbps 付近で止まっている。`speedtest.Capped100M` の幅 (80〜100 Mbps) に入るので、画面に
  「🚨 100 Mbps 付近で頭打ち」が出る。ms との経路のどこかに 100M の機器・ケーブルがある疑い (ssh の暗号化は 100 Mbps より十分速い前提。未実測)
- 追加の依頼 (同じ日): CLI からも測れるように (`speedtest` のサブコマンド) / 最下行に `t` の案内 (`?` の一覧には在った) — commit
  「mac-lan-manager: speedtest のサブコマンドで管理対象との通信速度を CLI から測る・最下行に t の案内を出す」
  - CLI の敵対的レビュー (opus、3 周): 1 周目 P2 2 件 (曖昧な名前で別の Mac を黙って測る / 走査が --wait までに終わらなくても黙って
    途中の一覧で測る) を直した (曖昧なら rc=2・scan_settled)。2 周目 P2 1 件 (Bonjour の走査そのものの失敗が scan_settled=true に隠れる) を
    直した (scan_error・rc=1)。P3 は名前の大文字・末尾の . を揃える、注記の文言、測定中の t の案内を直した
  - 受容: 画面の t で断る文言の順が変わった (SpeedTest が無い画面・測定中の判定が先。測ってしまう形は無い) / 台帳の内部 IP だけで相手がまだ
    見えていない回は経路が空 (link="") で 100M の頭打ちを判定しない (scan_settled / link で読む)
    / 3 周目 (P1・P2 無し): smb / rfb の走査だけが落ちても rc=1 (Bonjour の走査が欠けたら不調と読む設計) / 測定中に起きた走査の失敗は rc に
    入らない (宛先は決めた後で、測った値は正しい)。大文字の短い名前の経路のテストを足した
- 2026-10-18 (追記): 板で経路を選べるようにした (既定は有線。`t` → 経路の一覧で `▶` が内部 IP の `(有線)` → `y`、`j` で Wi-Fi の IP)。トーストと結果に IP と線が出る。CLI の `speedtest` は今までどおり繋ぐ先 (connectIP) を測る
