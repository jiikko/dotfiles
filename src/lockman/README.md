# lockman

ディレクトリ単位の排他を取る CLI。SMB 越しの複数マシンと、共有を公開しているホストの
ローカル経路が**混在**しても同じ排他が効くことを狙う。

**仕様の正本は [issues/091-feat-lockman-directory-lease-lock.md](../../issues/091-feat-lockman-directory-lease-lock.md)。**
ここには実装側の事情だけを書く。

## 現状

**骨組みだけ。全サブコマンドが未実装で exit 125 を返す。**

実装に入る前に、issue の「実測が要る前提」を macOS 実機で測ること (推論で進めない):

1. `link(2)` が smbfs 越しに使えるか (ENOTSUP の公算が高い → `O_EXCL` 経路が本線)
2. 書き込み後の mtime を打刻するのは公開ホストか、クライアントか
3. smbfs が `fcntl` byte-range lock をサーバへ送るか
4. 属性・ディレクトリキャッシュの実際の遅れ

## 対象環境

macOS のみ (クライアント = smbfs、公開ホスト = macOS のファイル共有)。**Windows 非対応**、
Linux / Samba も想定しない。ただし **CI は ubuntu で回る**ため、コード自体は linux でも
ビルド・テストできる状態に保つこと。

⚠️ **CI が検証するのはローカル FS 上の挙動だけ**。SMB 由来の前提 (キャッシュ・打刻・
ロック転送) は CI では一切検証されない。実機検証は `human` issue で人が行う。

## 開発

```sh
make -C src/lockman lint   # golangci-lint (go run 経由・バージョン固定)
make -C src/lockman test   # go test -race
```

root の `make test` にも含まれる (`GO_PROJECT_DIRS`)。

- **`go.sum` は空だが消さないこと**。依存は今のところ標準ライブラリだけだが、CI の
  `actions/setup-go` が `cache-dependency-path: src/lockman/go.sum` を解決できないと
  ジョブごと失敗する。依存を足せば中身が入る
- `bin/lockman` ラッパを作るときは `bin/lib/go_autobuild.zsh` 方式に合わせる。ただし
  **`--async` は使わない** — glogx は popup の体感速度のため「旧版で即起動」を選んで
  いるが、排他の道具で古いバイナリが動くのは危険なので同期ビルドにする
