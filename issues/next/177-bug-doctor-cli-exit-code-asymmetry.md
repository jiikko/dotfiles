# 177 bug: CLI の exit code が「診断できず」を伝え落とす (svcdoctor の BrewErr / -json / 語彙の非対称)

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 5 / 体 6) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (4 章「出力は検出とコマンド提示まで」/ 2 章「検出の健全性」)

## 対象

`src/doctor/cmd/svcdoctor/main.go` の exit 判定 switch、同 `-json` 経路、`src/doctor/cmd/diskdoctor/main.go`

## 何が起きるか

3 つの穴がある。どれも「検査できなかった」を exit code で受け取れないので、
スクリプト・CI から呼んだときに false green になる。

### (a) svcdoctor は brew 台帳が取れなくても exit 0 (実証済み)

exit 判定は `Interrupted || StatusErr || Undiagnosed || DirErrs` だけで **`rep.BrewErr` を見ない**。

実測 (体 5。偽 HOME + brew を PATH から外した環境):

- text 出力: rc=**0**。画面には `🚨 診断できず (brew): exec: "brew": executable file not found in $PATH` と
  **`壊れた登録は見つかりませんでした`** が**同時に**出る
- `-json`: rc=0、`BrewErr` 非空、`Findings` null

diskdoctor は failed があれば exit 2 に倒れるので、**非対称**。

### (b) svcdoctor の `-json` は常に exit 0

`-json` 経路は `return` で exit 判定を飛ばす。JSON を読む側 (将来の glogx 連携・スクリプト) は
「診断できず」を exit code で見られない。diskdoctor の `-json` は exit 2 を維持するので、ここも非対称。

### (c) exit code の語彙が CLI 間で違う

| CLI | 候補あり | 診断できず |
|---|---|---|
| svcdoctor | 1 | (0 のまま = 上記 a) |
| diskdoctor | **0** | 2 |

UI の色 (赤=要確認 / 黄=注意・診断できず / 緑=安全) との対応表も無い。

## 再現手順

```
env -i PATH=/usr/bin:/bin HOME=$(mktemp -d) <svcdoctor>; echo rc=$?      # rc=0 なのに「診断できず」が出る
env -i PATH=/usr/bin:/bin HOME=$(mktemp -d) <svcdoctor> -json; echo rc=$?
```

## 対応案

- svcdoctor の exit 判定に `BrewErr != ""` を足す
- `-json` でも同じ exit 判定を通す (両 CLI)
- **0 / 1 / 2 の意味を両 CLI で揃え、`--help` に書く** (0=候補なし / 1=候補あり / 2=診断できず、等)。
  併せて issue 181 (入口ドキュメント) の `--help` 修正と同じ変更でやる
- 候補 0 件のとき diskdoctor が見出しだけを出す (UI と svc CLI は「候補はありません」を出す) のも同じ変更で揃える

## 受け入れ条件

- [ ] BrewErr のある svcdoctor が非 0 で終わる (偽環境のテスト)
- [ ] `-json` でも exit code が同じになる
- [ ] `--help` に 0/1/2 の意味が書かれている
- [ ] 変異検証: 判定条件を外すと rc=0 に戻ることを確認する
