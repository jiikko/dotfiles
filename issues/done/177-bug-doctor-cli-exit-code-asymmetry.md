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

## 対応 (2026-09-03)

**修正した。(a)(b)(c) すべてと、「候補 0 件のとき見出しだけ」も直した。**

### 終了コードの語彙 (2 本で共通)

| code | 意味 |
| --- | --- |
| `0` | 診断できた + 候補なし |
| `1` | 診断できた + 候補あり |
| `2` | 引数が不正、または診断できなかったものがある (**`2` が `1` より優先**) |
| `3` | 実行環境・出力の失敗 (home の解決 / `-json` のエンコード) |

`2` を `1` より優先するのは、「候補が 1 件あった」より「一部を検査できなかった」の方が
呼び出し側が知る必要のある事実だから (見えていない候補があるかもしれない)。

### 構造で直した 3 点

1. **定数を共有 package `doctor/exitcode` に 1 箇所だけ置く**。当初は各 CLI にコピーし、
   `TestExitCodeVocabulary` で守ったつもりだったが、**別 package なので参照が切れており、
   svcdoctor 側の値を 9 に変えても全テストが green のまま通った** (敵対レビューが実測)。
   テストではなく構造で直した
2. **出力と終了コードを `emit(rep, jsonOut, ..., stdout, stderr) int` に切り出し、
   終了コードを出力の分岐の外で決める**。issue 177 (b) の「`-json` が `return` で判定を飛ばす」形が
   構造的に書けなくなる。`main` は `os.Exit(emit(...))` だけ
3. `svcExitCode` が **`rep.BrewErr != ""` を見る** ((a))。`diskExitCode` は候補ありを `1` で返し、
   `failures` (エントリ内の一部 Item が読めない) も `2` に倒す ((c))

`disk.Format` は候補 0 件のとき「掃除の候補はありませんでした」を出す (UI / svcdoctor と揃えた)。
`src/doctor/README.md` の終了コード表と両 CLI の `--help` を同じ commit で追従させ、
`issues/done/181` にも追記した。

### A/B 実測 (issue の再現手順そのもの)

偽 HOME に `homebrew.mxcl.redis.plist` を置き、`env -i PATH=/usr/bin:/bin` で brew を PATH から外す:

| | 修正前 (HEAD のバイナリ) | 修正後 |
| --- | --- | --- |
| `svcdoctor` (text) | rc=**0** | rc=**2** |
| `svcdoctor -json` | rc=**0** | rc=**2** (`BrewErr` に `exec: "brew": executable file not found in $PATH`) |

### 変異検証

- `-json` が判定を飛ばす旧挙動へ戻す → red (`text=1 json=0 で食い違う`。両 CLI で複数ケースが個別に反応)
- `enc.Encode` の error を握り潰す → `failWriter` のケースだけ red
- 候補 0 件の行を消す / 常に出すようにする → **両方向とも** red
- `exitcode` の定数を各 CLI にコピーし直して片方だけずらす → 構造上できない (コピーが 0 件であることを grep で確認)

### 敵対的レビュー (sonnet / read-only / 2 周)

1 周目 5 観点: 採用 1 / 却下 0 / 記録 3。

- **採用 (P1 相当)**: `TestExitCodeVocabulary` が cross-CLI parity を守っていない (上記 1)
- **壊せなかった (最重要)**: **既存の呼び出し元が壊れないか**。`bin/` のラッパーは `exec` するだけ、
  `.github/workflows/doctor.yml` は `set +e` で `rc == 2` だけを assert (変更後も成立)、
  `Makefile` は実行しない、glogx は CLI を叩かず package を直接呼ぶ。
  2 周目では**実際にラッパー経由でビルドして実行**し、CI の smoke シナリオも再現して確認した
- **記録**: `main()` の配線がテストされていない → 上記 2 で構造化 + テスト追加
- **記録**: `flag.NArg() > 0` の裸の `2` → `exitcode.Undiagnosed` に置き換え

2 周目 5 観点: 採用 1 / 却下 0 / 別 issue 1。

- **採用**: `exitcode_test.go` の `Undiagnosed <= Findings` は**論理的に発火しえない** (直前の表が
  数字を厳密に固定しているため) うえ、コメントの「判定側がこの順序に依存する」が誤り
  (実際の優先順位は `if` の順序で実装されている) → assert を削り、何が優先順位を守っているかを
  コメントに書いた
- **別 issue へ**: `svcdoctor` は HOME が解決できないと `svc.Scan` を呼ぶ前に落ちるので、
  HOME に依存しない `/Library/LaunchAgents` と `/Library/LaunchDaemons` の診断まで失う。
  exit code (2 と 3) はそれぞれの定義どおり正しく、直すべきは挙動の方なので
  [issues/191](191-bug-svcdoctor-home-failure-aborts-whole-scan.md) に切り出した
- **壊せなかった**: `emit` の構造が issue 177 (b) の形を禁じているか (変異で両 CLI とも red) /
  共有 package 化による import cycle・ラッパーのビルド・CI / 新規テスト 3 本の vacuous 性

### 記録 (受容した指摘)

- 「`emit` に `os.Exit` を直接書いたり、3 つ目の分岐 (`-quiet` 等) を足せば構造は破れる」→ **受容**。
  「構造的に禁じている」のは**今の 2 分岐が 1 つの判定関数に合流している**ことで、
  コンパイラが強制するわけではない。issue 177 (b) の形は塞げている
