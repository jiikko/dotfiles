# 768 (refactor): pro-con の presence と relay の「flock した印」の作り方と掃除を 1 か所にする

起票日: 2026-10-15

## 概要

`pro-con/presence/presence.go` と `pro-con/relay/relay.go` が、同じ手順の「生きている間 flock を持ち続ける印」を別々に書いている。
この手順の順序は敵対的レビューで直したもので、relay のコメントにも「presence と同じ形」とある。同じ不変条件の正本が 2 つあり、片方だけ直す形になりうる。

## 詳細 (2026-10-15 に本文で確認)

- 共通の手順: `.tmp-<id>` を `O_CREATE|O_EXCL` で作る → `flock(LOCK_EX|LOCK_NB)` → `<id>.lock` へ rename。
  1 分より古い `.tmp-` を掃除する (`staleTmp = time.Minute` が presence.go と relay.go に 1 つずつある)
- 違い:
  - id のバイト数 (8 と 6)
  - presence は rename の前に JSON を書く
  - 他人の印の確かめ方: presence は `LOCK_EX` を取って、誰も持っていなければ印を消す / relay は `LOCK_SH` で確かめるだけで何も変えない (読む側の契約)

## 対応方針

- pro-con の中に小さな package を作り、「印を作る」と「古い作りかけを掃除する」だけを共有する。確かめ方 (EX で消す / SH で見るだけ) は呼び出し側に残す
- 寄せる前に、2 つの呼び出し元のコメントの 🚨・「〜しない」と前提 (順序・掃除の時間) を表にして、共有する側がその全部を表せるかを確かめる
- 既存のテスト (落ちた画面の残骸・同時に開く) が両方の呼び出し元で残ることを変異で確かめる

## 関連ファイル

- `pro-con/presence/presence.go` (`OpenAs` / `heldBySomeone` / `scan`) / `pro-con/relay/relay.go` (Writer の生成 / `held` / `sweep`)
- 調査の全体: 773
