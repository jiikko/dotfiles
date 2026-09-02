# doctor — ディスク掃除候補と壊れた launchd 登録の診断

「消してよさそうなもの」と「壊れて残っている常駐」を**見つけて説明するだけ**のライブラリと CLI 2 本。
**削除・停止は一切しない** (dry-run のみ)。実行するコマンドは提示するので、判断と実行は人が行う。

同じ検査は glogx の doctor 画面 (`D`) からも呼ばれる。

## 2 つの CLI の使い分け

| | `bin/diskdoctor` | `bin/svcdoctor` |
| --- | --- | --- |
| **見るもの** | **容量** — allowlist に載った掃除候補が今どれだけ占有しているか | **常駐** — 壊れた launchd 登録 (実行ファイル不在 / 失敗し続け / Homebrew 台帳に無い) |
| 使いどき | ディスクが減ったとき | 常駐が増えた / 起動が遅い / 見覚えのないサービスがあるとき |
| 出力の並び | 占有量の降順 (リスクと復元方法つき) | 検出した登録ごとに、判定理由と手で叩くコマンド |

迷ったら「**容量が知りたいなら diskdoctor、常駐が知りたいなら svcdoctor**」。両者は対象が
重ならないので、片方の結果からもう片方を推測できない。

## 終了コード (2 本で非対称)

`--help` にも同じ内容がある。**「検査できなかった」を緑にしない**のが共通の設計。

| | 0 | 1 | 2 |
| --- | --- | --- | --- |
| `diskdoctor` | 一覧を出せた (**候補の有無では変わらない**) | 出力に失敗 (`-json` のエンコード) | 引数が不正 / 走査できなかったエントリがある |
| `svcdoctor` | 候補なし | **候補あり** (または home 解決・出力の失敗) | 引数が不正 / 診断できなかったものがある |

⚠️ **既知の非対称** (issue 177 で是正予定):

- 「候補あり」を `diskdoctor` は 0、`svcdoctor` は 1 で返す
- `svcdoctor -json` は候補・未診断があっても 0 を返す (JSON を書いた時点で return する)

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
| `brewledger/` | Homebrew が管理している formula/cask の台帳。**disk (`brew-orphan-state`) と svc (`homebrew.mxcl.<formula>` が台帳に無い判定) が同じ集合を引く**ための共有パッケージ |
| `cachedir/` | キャッシュ置き場 (`$XDG_CACHE_HOME/glog`、未設定なら `~/.cache/glog`) の解決。glogx 本体と doctor のスキャン結果で共有する (⚠️ ディレクトリ名は `glogx` ではなく `glog`) |
| `runner/` | 外部コマンドの実行口。**stdout / stderr / exit code を分けて返す** (混ぜるとどの stream が判定材料か確定できない)。テストではここを差し替える |

`glogx` からは go.mod の `replace doctor => ../doctor` で参照する。

## 設計の要点

- **削除の入口を持たない**ことが「実行しない」の担保。停止・削除のフラグは無く、
  提示するのは人が読んで叩くコマンドだけ
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
  ⚠️ PATH から外すだけでは足りない (`xcrun` は `/usr/bin` にあるので実在して成功する)
