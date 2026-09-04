# doctor — ディスク掃除候補と壊れた launchd 登録の診断

「消してよさそうなもの」と「壊れて残っている常駐」を見つけて説明するライブラリと CLI 2 本。

- **CLI (`bin/diskdoctor` / `bin/svcdoctor`) は削除・停止を一切しない** (dry-run のみ)。
  実行するコマンドは提示するので、判断と実行は人が行う
- **このライブラリで削除するのは `disk.Delete` だけ**で、その唯一の呼び出し元は
  glogx の doctor 画面 (`D` → `Space` で選択 → `d` → 確認 → `y`)。サービス診断 (`svc`) と
  Docker 診断 (`docker`) は削除の関数を持たない。
  🚨 ただし **`docker` が提示するコマンドは glogx から実行できる** (`Space` → `x` → 確認 → `y`)。
  実行するのは画面が持つ汎用のコマンド実行機構 (brew の手と共有) で、このライブラリは
  文字列を組むだけ。**ボリュームは対象外** (中身はデータで戻らないため、コピーのみ)。不変条件は `disk/delete.go` の冒頭が正本 (issues/148 の ④ 節)

## 2 つの CLI の使い分け

| | `bin/diskdoctor` | `bin/svcdoctor` |
| --- | --- | --- |
| **見るもの** | **容量** — allowlist に載った掃除候補が今どれだけ占有しているか | **常駐** — 壊れた launchd 登録 (実行ファイル不在 / 失敗し続け / Homebrew 台帳に無い) |
| 使いどき | ディスクが減ったとき | 常駐が増えた / 起動が遅い / 見覚えのないサービスがあるとき |
| 出力の並び | 占有量の降順 (リスクと復元方法つき) | 検出した登録ごとに、判定理由と手で叩くコマンド |

迷ったら「**容量が知りたいなら diskdoctor、常駐が知りたいなら svcdoctor**」。両者は対象が
重ならないので、片方の結果からもう片方を推測できない。

## 終了コード (2 本で共通の語彙)

`--help` にも同じ内容がある。**「検査できなかった」を緑にしない**のが共通の設計。

| code | 意味 |
| --- | --- |
| `0` | 診断できた + 候補なし |
| `1` | 診断できた + 候補あり |
| `2` | 引数が不正、または診断できなかったものがある (**`2` が `1` より優先**) |
| `3` | 実行環境・出力の失敗 (home の解決 / `-json` のエンコード) |

`2` を `1` より優先するのは、「候補が 1 件あった」より「一部を検査できなかった」の方が
呼び出し側が知る必要のある事実だから (見えていない候補があるかもしれない)。

**`-json` でも同じ終了コードを返す** (両 CLI)。JSON を読む側も「診断できず」を exit code で受け取れる。

「診断できず」に倒れる条件:

| CLI | 条件 |
| --- | --- |
| `diskdoctor` | エントリが `failed` / エントリ内の一部の Item を走査できず (`failures`) / 中断 (`partial`) |
| `svcdoctor` | 中断 / `launchctl` の失敗 / **brew 台帳が取れない (`BrewErr`)** / 未診断あり / 読めないディレクトリ |

判定は `cmd/diskdoctor/exit.go` の `diskExitCode` と `cmd/svcdoctor/exit.go` の `svcExitCode` に
純関数として置いてある (テストは同ディレクトリの `exit_test.go`)。issue 177 まではここが
2 本で非対称で、`svcdoctor` は `BrewErr` を見ておらず、`-json` は判定ごと飛ばしていた。

## glogx の doctor 画面との関係

`glogx` の `D` は、この 2 つの検査を同じ画面に並べて出す (`src/glogx/doctor_view.go` が
`doctor/disk` と `doctor/svc` を直接呼ぶ。CLI を起動するのではない)。
画面内のキーとキャッシュの規律は [`src/glogx/README.md`](../glogx/README.md) のキー表を参照。

CLI が要るのは「スクリプトから叩きたい」「JSON で受けたい」ときで、日常の確認は画面の方が速い。

## パッケージ構成

| パッケージ | 役割 |
| --- | --- |
| `disk/` | 掃除候補の allowlist (`catalog.go`)、走査 (`scan.go`)、整形 (`report.go`)、除外判定 (`guard.go` — 起動中プロセス・boot 時刻・現存する simulator デバイスを見て「今は消してはいけない」を弾く。**判定に失敗したら fail-closed** = 対象外へ倒す) |
| `svc/` | launchd の plist 読み (`plist.go`)、`launchctl` 経由の状態取得 (`launchctl.go`)、Homebrew 台帳との突き合わせ (`brew.go`)、整形 (`report.go`) |
| `docker/` | Docker Desktop が抱えている未使用資源 (停止コンテナ / 未参照イメージ / ビルドキャッシュ / 参照の無いボリューム)。**削除の経路は持たない** — 提示するのは `docker ... prune` のコマンドだけ。合計と回収可能量は `docker system df` の申告をそのまま使う (共有レイヤーの勘定を自前で作り直さない) |
| `brewledger/` | Homebrew が管理している formula/cask の台帳。**disk (`brew-orphan-state`) と svc (`homebrew.mxcl.<formula>` が台帳に無い判定) が同じ集合を引く**ための共有パッケージ |
| `cachedir/` | キャッシュ置き場 (`$XDG_CACHE_HOME/glog`、未設定なら `~/.cache/glog`) の解決。glogx 本体と doctor のスキャン結果で共有する (🚨 ディレクトリ名は `glogx` ではなく `glog`) |
| `internal/displaycheck/` | 「表示用の構造体へ文字列フィールドを足したのに `Sanitize*ForDisplay` へ通し忘れる」を止める検査の本体 (issue 252)。`disk` / `svc` / `docker` の 3 つが**同じ判定を 1 実装で共有**し、各 package は関門表と免除表だけを渡す。検査器自身のテストは testdata の fixture で持つ |
| `runner/` | 外部コマンドの実行口。**stdout / stderr / exit code を分けて返す** (混ぜるとどの stream が判定材料か確定できない)。テストではここを差し替える |

`glogx` からは go.mod の `replace doctor => ../doctor` で参照する。無害化の関門
(`src/termsafe`) は逆に**この module が取り込む**側 (`replace termsafe => ../termsafe`)。

## 表示に出す前の関門 (`SanitizeForDisplay`)

走査した値は**すべて自分以外が書いた文字列** — パスはファイル名由来 (macOS のファイル名は
`/` と NUL 以外の任意バイトを許す)、`Contents` は `os.ReadDir` の名前、`svc` の Label は
plist の中身、`Reason` は OS のエラー文。対象は `$TMPDIR` や `~/Library/LaunchAgents` の
ような**誰でも書ける場所**なので、細工した名前を 1 つ置くだけで注入できる。

🚨 **CLI は stdout へ直接書く** = TUI と違って後段が無いので、OSC52 (クリップボード書き込み) や
タイトル書き換えが「表示しただけ」で発火する。そこで:

- `disk.SanitizeForDisplay` / `svc.SanitizeForDisplay` を **`Format` の先頭で通す**
  (glogx の doctor 画面も同じ関数を受け口で通す = 判定を 2 実装持たない。issue 228)
- 🚨 **`disk` の `Item.Path` は書き換えず、制御文字を含むものを落とす** (落とした件数は
  `Failures` に残し、合計からも引く)。書き換えると `planDelete` の照合 (`itemKey`) から外れ、
  「画面に出ているものと消えるものが違う」を作る
- 削除の**記録** (`DeleteReport`) は逆にパスを書き換える (`SanitizeDeleteReportForDisplay`)。
  もう照合には使わないので、落とすと「触ったのに一覧に出ない対象」ができて記録が嘘になる

## 設計の要点

- **CLI が削除の入口を持たない**ことが「CLI は実行しない」の担保。停止・削除のフラグは無く、
  提示するのは人が読んで叩くコマンドだけ。削除は `disk.Delete` (ライブラリ) にだけあり、
  破壊的操作を持つファイルは `disk/delete.go` の 1 本に閉じている
- **判定できなかったものを合計に足さない** (`blocked` / `failed` は別扱いで、
  終了コードにも出す)。「見えなかったもの」を「無かったもの」に丸めない
- 外部コマンドは `runner` 越しに呼ぶので、テストは実 `launchctl` / `du` なしで書ける

## テストと CI

```
make -C src/doctor lint
make -C src/doctor test
```

CI は 3 レーンに分かれている。**doctor 機能を触ったら見るのは `doctor` レーン**。

| レーン | 見るもの | 実測 |
|---|---|---|
| [`doctor.yml`](../../.github/workflows/doctor.yml) | **doctor 機能の横断レーン**。この module のテスト + glogx 側の doctor 配線テストだけ + 2 CLI のビルド + 依存コマンド失敗時に fail-closed へ倒れる smoke | glogx 側は 1 秒 |
| [`src_doctor.yml`](../../.github/workflows/src_doctor.yml) | この module 単体の lint + test (module の正本) | — |
| [`src_glogx.yml`](../../.github/workflows/src_glogx.yml) | glogx 全体の lint + test (doctor 以外の退行を見る。`replace` で取り込むのでここも走る) | 31 秒 |

`doctor.yml` が持つ 2 つの gate は、どちらも「素通りする形」を止めるためにある。

- **テスト本数の下限**: `go test -run 'TestDoctor'` は**1 本も走らなくても rc=0** を返す。
  パターンとテスト名が食い違うと vacuous pass になるので、`--- PASS: TestDoctor` の本数を数えて
  下限 (15 本。実測 19 本) を課している。テスト名の頭を変えるならこの gate も直す
- **fail-closed の smoke**: `xcrun` / `brew` を**必ず失敗する偽物**に差し替えて `diskdoctor -json` を回し、
  依存コマンドを使う 4 判定が `failed` (診断できず) になることを見る。
  🚨 PATH から外すだけでは足りない (`xcrun` は `/usr/bin` にあるので実在して成功する)
