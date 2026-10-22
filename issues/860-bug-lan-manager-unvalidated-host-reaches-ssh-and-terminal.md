# 860 (bug): Bonjour の名前が未検査のまま ssh の HostKeyAlias と端末へ届く

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

起票日: 2026-10-22

## 概要

lan-manager の全体監査 (865) の security。LAN 上の機器が広告する名前 (Bonjour) と、相手 Mac から読んだ値が、
検査や無害化を通らずに ssh のオプションと端末へ届く経路が 2 つある。

## 詳細

### 1. `s` の HostKeyAlias (P2・ssh 側の書き込みの形は未確認)

- `lanmgr/model.go:keyList` の `case "s"` は `"HostKeyAlias=" + r.host` をその場で組む。`setup.Dest.Validate` は同じ値を
  `reAlias` (`^[A-Za-z0-9._-]+$`) で絞り、理由を「known_hosts の行は `,` で名前を並べる・`*` `?` `[` はパターン」と書いている。
  `s` の経路だけがこの検査を通らない。`rows()` の入口は `termsafe.IsPlain` だけで、`,` を通す
- 経路: `discover/discover.go` の `reLookup` は宛先を `(\S+?)` で取るので `,` を通す
  1. LAN 上の機器が `_ssh._tcp` を広告し、SRV の宛先を `x,m2macmini.local` にする
  2. 利用者がその未管理の行で `s` を押し、初めての鍵の確認に yes と答える
  3. known_hosts に `x,m2macmini.local <攻撃者の鍵>` が書かれる (と読んでいる。**ssh がこの形で書くかは未確認** — 確かめるには接続が要る。
     man ssh_config は HostKeyAlias を鍵の保存にも使うと書く)
  4. 以後 `HostKeyAlias=m2macmini.local` の照合 (s・初期設定・速度) を攻撃者の鍵が通る
- 確かめたこと (反証レビュー、OpenSSH_10.3p1): `ssh -G -o HostKeyAlias=x,m2macmini.local` は値をそのまま受け付ける。
  偽の known_hosts で `x,m2macmini.local <key>` の行に `ssh-keygen -F m2macmini.local` が rc=0 で一致する。
  `,` だけでなく `*` も通り、`*.local <key>` の行も `m2macmini.local` に一致する (全部の .local に効く鍵を置ける形)
- 構造の側 (宛先と名前が生の string のまま越境し、検証が経路ごとに違う) は 863 の P6

### 2. `dump` の文字の出力 (P3)

- `dump.go:dumpPeer` の文字の出力と `writeDumpText` の「# 知らせ」は、相手の networksetup のサービス名・方式・ルーターと ssh の stderr を
  無害化せずに stdout へ出す。画面の側は termsafe を通している
- 経路: 壊れた・乗っ取られた相手 Mac がサービス名に ESC / OSC (OSC 52 など) を入れ、利用者が README の案内どおり `dump --peer` を打つ

## 対応方針

- 1: `s` の alias に `reAlias` の述語を通し、通らなければ理由を出して断る。`,` と `*` を含む名前のテストを足す。
  `setup.Dest{}.Validate()` を丸ごと通すのは採らない (IP の欄の IPv4 検査が、`m.target(r)` が返す有線の名前 `*.wired.local` を断り、有線の名前で繋ぐ s を壊す)
- 2: 文字の出力 (JSON でない方) を `termsafe.PlainLine` に通す

## 関連ファイル

- `src/mac-lan-manager/lanmgr/model.go` (`keyList` の `s`)、`setup/remote.go` (`Dest.Validate` / `reAlias`)、`discover/discover.go` (`reLookup`)
- `src/mac-lan-manager/dump.go` (`dumpPeer` / `writeDumpText`)

## 進捗

- [ ] 1 の修正とテスト
- [ ] 2 の修正とテスト
