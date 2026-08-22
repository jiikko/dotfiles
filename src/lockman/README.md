# lockman

ディレクトリ単位の排他を取る CLI。SMB 越しの複数マシンと、共有を公開しているホストの
ローカル経路が**混在**しても同じ排他が効くことを狙う。

**仕様の正本は [issues/091-feat-lockman-directory-lease-lock.md](../../issues/091-feat-lockman-directory-lease-lock.md)。**
ここには実装側の事情だけを書く。

## 現状

**ローカル FS 上で動く v1 が入っている** (`acquire` / `release` / `renew` / `check` /
`status` / `with` / `break` / `cleanup`)。**SMB 越しの検証はまだ**。

実装の要点 (崩すと排他が消える。変更時は必ずテストの変異検証まで戻ること):

- 勝敗は「存在すれば失敗する 1 回の原子操作」だけで決める (`link(2)` → 駄目なら
  `O_CREAT|O_EXCL`)。**事前に存在チェックをしない**
- 期限切れの引き継ぎは `unlink` ではなく**存在しない名前への `rename`** で勝者を 1 人に
  絞る (この 1 行を `unlink` に変える変異でテストが「勝者が 2 人」で赤になることを確認済み)
- TTL 判定に**ローカル時計を使わない**。`probe/` にファイルを作って mtime を読み、
  公開ホストの時計を出典にする
- 判定不能 (壊れた lock・時刻が取れない・I/O が返らない) は **busy / エラー側へ倒す**

v1 で**あえて入れていない**もの:

- **同一マシンの pid 生存による即時 stale 判定**。`IOPlatformUUID` / `kern.boottime` の
  取得に外部依存が要り、効果は「回収が TTL より早くなる」だけ。安全側 (常に TTL を待つ)
  なので後回しにした
- **`bin/lockman` ラッパ**。まだ入れていない (下の「開発」参照)

実機で測っていない前提 (issue 091 の「実測が要る前提」):

1. `link(2)` が smbfs 越しに使えるか (ENOTSUP なら `O_EXCL` 経路に落ちる。実装は自動で追従)
2. 書き込み後の mtime を打刻するのは公開ホストか、クライアントか
   (`renew` が毎回検算し、ずれていればエラーで止まる)
3. smbfs が `fcntl` byte-range lock をサーバへ送るか (v1 は使っていない)
4. 属性・ディレクトリキャッシュの実際の遅れ (TTL 下限 30 秒の根拠)

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
