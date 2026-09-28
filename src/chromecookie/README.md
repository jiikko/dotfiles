# chromecookie

macOS の Google Chrome のプロファイルから Cookie を復号して取り出す Go ライブラリ。
[slack-cli](https://github.com/jiikko/slack-cli) / [esa-cli](https://github.com/jiikko/esa-cli) /
[newrelic-nrql-cli](https://github.com/jiikko/newrelic-nrql-cli) が共有する。以前は 3 repo にコピーがあり、
修正が片方にしか当たらずに分岐していた。**Cookie 復号の修正はここへ入れ、3 repo 側にコピーを戻さない**。

- macOS + Google Chrome 専用 (他のブラウザ・OS の Keychain サービス名やディレクトリ名は実機で確かめられないため載せない)
- 復号: Keychain `Chrome Safe Storage` → PBKDF2-SHA1 (1003 回, `saltysalt`, 16B) → AES-128-CBC (IV=0x20×16) → PKCS7。
  `meta.version >= 24` なら先頭 32B が `SHA256(host_key)` であることを照合してから落とす

## 取り込み方

tag は打たない (master を最新として使う)。3 repo の CI / goreleaser がそのまま取れるように、`replace` ではなく通常の依存にする。

```sh
go get github.com/jiikko/dotfiles/src/chromecookie@master   # pseudo-version で go.mod に固定される
```

ここを直したら、使う側の repo で上のコマンドを打ち直して go.mod / go.sum を更新する。

## 使い方

```go
ws := chromecookie.NewWorkspace("esa-cli") // 一時コピーの作業領域 ~/Library/Caches/esa-cli/extract
ws.InstallCleanupOnSignal()                 // main の先頭で 1 回 (②: シグナルで終わっても一時コピーを消す)
ws.SweepStaleTempDirs()                     // main の先頭で 1 回 (③: SIGKILL 等で残った残骸を消す)

pw, err := chromecookie.KeychainPassword() // *EnvError なら即停止
res, err := ws.ReadCookies(profile, pw)     // names を渡すとその名前だけ読む
// 目的の Cookie が無かったら、原因の手がかりを確かめる
if err := res.Diagnose("esa 宛ての Cookie "); err != nil { /* *ReadError: 記録して次のプロファイルへ */ }
```

複数のプロファイルを順に試すときのエラーの扱い (package doc が正本):

| エラー | 扱い |
|---|---|
| `*EnvError` (`IsEnvError`) — Keychain の鍵を取れない / 作業領域の異常 / macOS 以外 | 即停止 (どのプロファイルでも同じ) |
| `IsMissing` — Cookie DB が無い / `Diagnose` が nil (Cookie 0 件) | 黙って skip |
| `*ReadError` — アクセス拒否・壊れた DB・全件復号失敗・`-wal` / `-shm` を読めない | 記録して skip し、全滅したら `IssueNote` で理由を添える |
| それ以外 | 止める側に倒す (分類は許可リスト) |

`ListProfiles` は Local State からプロファイルを列挙する (直近に使ったものが先頭)。
Cookie DB 以外のファイル (slack-cli の Local Storage など) を一時コピーするときも `ws.NewTempDir` を通す
(通さないと②③の後始末の対象にならない)。どの Cookie を選ぶか・案内に出すフラグ名は使う側が持つ。
