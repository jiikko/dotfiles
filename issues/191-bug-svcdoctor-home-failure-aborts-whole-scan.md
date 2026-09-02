# 191 bug: svcdoctor は HOME が解決できないと走査ごと中止し、HOME に依存しない 2 ディレクトリの診断まで失う

起票日: 2026-09-03
重要度: **P3**
関連: [issues/done/177](done/177-bug-doctor-cli-exit-code-asymmetry.md) (敵対レビュー 2 周目の指摘) /
[issues/done/175](done/175-bug-doctor-expand-silent-zero-on-glob-meta-and-relative.md) (diskdoctor 側の対応)

## 対象

`src/doctor/cmd/svcdoctor/main.go` の `os.UserHomeDir()` 失敗時の `os.Exit(exitcode.EnvFailure)`

## 何が起きるか

`svc.DefaultDirs(home, uid)` が返す 3 つのうち、**`home` を必要とするのは `$HOME/Library/LaunchAgents` の 1 つだけ**で、
残る `/Library/LaunchAgents` と `/Library/LaunchDaemons` は絶対パスなので HOME と無関係。
それなのに `main()` は `svc.Scan` を呼ぶ前に落ちるので、**HOME と関係のない 2 ディレクトリの診断まで失われる**。

実測 (2026-09-03、敵対レビューが `env -i PATH=/usr/bin:/bin <bin>` で採取。Go の unix 実装は `$HOME` を読むだけで
passwd への fallback が無いので、HOME 未設定なら確実に error になる):

| CLI | rc | stdout |
|---|---|---|
| `svcdoctor` | **3** | **空** (`svc.Scan` を呼ばずに中止。stderr に `svcdoctor: $HOME is not defined`) |
| `diskdoctor` | 2 | **出る** (HOME を要らないカタログエントリの候補まで出る。例: シミュレータランタイム 11.6GB) |

diskdoctor 側は issue 175 で `expand` が**エントリ単位**で「絶対パスでない / HOME が空」を弾く形にしたので、
HOME に依存しないエントリの診断は残る。svcdoctor だけが粗い。

## これは exit code の問題ではない

両方とも非 0 なので issue 177 の「検査できなかったを緑にしない」は守られている。
`3` (そもそも試行できなかった) と `2` (試行して一部が診断できず) はそれぞれの定義どおり正しい。
**直すべきは「HOME の失敗で走査ごと中止する」挙動**の方。

## 対応案

- `os.UserHomeDir()` の失敗を `svc.Options` へ渡し、`$HOME/Library/LaunchAgents` **だけ**を
  `DirErrs` に落として残る 2 ディレクトリの走査を続ける (`sizePaths` が TCC で読めない 1 件のために
  全部を隠さないのと同じ規律)
- そうすると rc は `2` になり、diskdoctor と揃う

## 受け入れ条件

- [ ] HOME 未設定で `/Library/LaunchAgents` と `/Library/LaunchDaemons` の診断が出る
- [ ] `$HOME/Library/LaunchAgents` が `DirErrs` に入り、rc が 2 になる
- [ ] 変異検証: `DirErrs` に落とさず握り潰すと rc が 0 になることを確認する
