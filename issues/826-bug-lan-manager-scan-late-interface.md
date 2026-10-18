# 826 (bug): lan-manager の走査・解釈: 後から上がった I/F の IP を拾わない ほか

> 🚨 **担当中: air5 (glogx)**（2026-10-18〜）

起票日: 2026-10-18

## 概要

`src/mac-lan-manager/` の `discover/`・`netinfo/`・`ledger/`・`prefs/` のバグ探し (2026-10-18、ユーザー依頼「lan-manager でバグがないか調べて」。opus の読み取り専用レビュー) で
見つかった誤動作 5 件 (P2 は無い。反証レビューで 1 を P3 に下げた)。issue 747 (done) の「残タスクの棚卸し」(ルーターの無い Wi-Fi が有線の判定を通る件など) と受容・記録済みの項目は除いてある。

## 詳細

1. **P3: 起動の後で上がった I/F で見えた相手の IP を、`r` を押すまで拾わない** (`discover/scan.go` の `onBrowse` の `seen` と、`resolve` の `newHost || refresh`)
   - -B が Wi-Fi (if 14) で `mini` を Add → -L と -G が走る (-G は 4 秒で止まる) → その後で有線 (if 27) でも同じ `mini` が Add される
     (ケーブルを後から挿した・相手の有線が後から上がった)。帳簿の鍵がサービス + インスタンスだけなので 2 本目の Add は捨てられ、-G は引き直されない。
     Rmv が来ないので `readded` も立たない。`Peer.Addrs` は `{14: …}` のままで有線の IP が載らない
   - 影響: 有線の内部 IP が出ない。「有線で見えた相手だけ」を有効にしていると相手が一覧から消える (行の `wired` は Addrs の I/F の番号から決まる)。
     初期設定でケーブルを繋いだ直後に踏みやすい
   - 確信度: 再現 (公開 API の `discover.Start` に偽の Starter を渡すテスト: -B の 1 行目は `Add 3 14 … mini`、300 ms 後に `Add 3 27 … mini`。偽の -G は有線が上がった後に
     呼ばれたときだけ if 27 の行も返す。1.5 秒後に `Addrs[27] != ""` を確かめる → `Addrs:map[14:…]`・-G は 1 回だけで FAIL)。main がコードで確認
   - 重要度: `r` で回復し、害は表示と「有線で見えた相手だけ」の絞り込みに限られる (別の IP へ繋ぐ類ではない) ので P3 とした
   - 747 で入れた重複除き (「同じインスタンスが 2 つの I/F で Add されると引き当てを二重に起こす → 重複を除く」) の副作用。直すときも同じ I/F の重複は除いたままにする
   - 直し方の案: Add を「サービス + インスタンス + I/F」で帳簿に付け、知らない I/F の Add が来たら -G だけ引き直す (同じ I/F の重複の Add は今どおり捨てる)
2. **P3: -L / -G が上限 (4 秒) 以内に応えないと、その走査の間は引き直さない** (`discover/scan.go`)
   - -L が空で返っても `seen[key]` は true のまま残り、その Mac は `r` まで一覧に出ない。-G が空で返ったホストは、別のサービスの引き当てでは `newHost = false` なので
     打ち直さず、IP の無い行のまま残る。747 の Add → Rmv → Add の件とは別 (一時的な無応答 1 回で、その走査の間ずっと欠ける)
   - 確信度: 取りこぼす経路はコードで確か。実際の頻度は未確認
3. **P3 (推測): 1 つの I/F で相手の IP が 2 つ見えると、片方が消え、応答の順で入れ替わりうる** (`discover/scan.go` の `p.Addrs[a.If] = a.IP`)
   - 鍵がこの Mac の I/F の番号だけなので、相手の Wi-Fi と有線が同じルーターの網に居て en0 に 2 本の A が届くと、後に来た方だけが残る
   - 確信度: 推測 (mDNSResponder が両方の I/F から応えるかは未実測)
4. **P3 (推測): `networksetup -getinfo` の方式が「Manually Using DHCP Router Configuration」「BOOTP Configuration」だと方式 "" と読む** (`netinfo/netinfo.go` の `ParseGetInfo`)
   - 判定は `Manual Configuration` / `DHCP Configuration` の部分一致だけ。相手の側は `setup/plan.go` で固定と見なされず固定し直しへ進み、`PeerStatic` の Validate が
     「戻す方式が分からない」で止まる (黙って壊れはしない)。この Mac の側は `SelfFixed` が "" を固定と見るので害は小さい
   - 確信度: 推測 (2 つの文字列はレビュアーの記憶。手元のサービスは全部 DHCP で実出力は取れていない)。直す前に `-setmanualwithdhcprouter` 等で実出力を採る
5. **P3: 台帳の行末にコメントを書くと、一覧の表示 (`Load`) ごと失敗する** (`ledger/ledger.go` の値の読み)
   - `alias = "x" # メモ` は `strconv.Unquote` が失敗し、緩い方の `ParseLenient` も err を返すので、台帳全体が読めなくなる。台帳は手で書き換えうる TOML 風の形式で、
     TOML では行末のコメントが合法。書き戻し側が断るのは正しいが、表示まで止まる
   - 確信度: コードで確認

### 範囲の外 (参考)

- `atomicfile.Write` (共有の package) は rename の前に fsync を打たない。電源が落ちると空の台帳になりえて、空のファイルは 0 台として正常に読まれる (推測)

### 見て問題が無かったもの (レビュアーの記録)

本物の出力と突き合わせ: `-listnetworkserviceorder` の `(*)` の行・スラッシュを含む名前 (`USB 10/100/1000 LAN`) / `-getinfo` が Router 行を出さない場合と `IPv6 Router` の行 /
`ifconfig` の `inet6` と `status:` / `ifconfig -l` の `vmenet0` と `bridge100` (IPv4 が無いので実害なし)。台帳の lock と読み直してから書く流れ・版の検査・緩い読みと
厳しい読みの使い分け・`prefs` の知らない種類名を残す扱いにも問題なし

## 関連ファイル

- `src/mac-lan-manager/discover/scan.go` / `netinfo/netinfo.go` / `ledger/ledger.go`
- issue 747 (done)

## 進捗

- [ ] 1〜5 を直す (4 は実出力を採ってから。3 は実測で起きるかを先に確かめる)

- 2026-10-18 反証レビュー (sonnet・読み取り専用): P1 なし・「既に直っている / 起きない」なし。記述の不正確さ (P3) を訂正した
