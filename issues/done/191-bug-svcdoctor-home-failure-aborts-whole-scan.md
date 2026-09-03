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

## 結論: 対応しない (ユーザー判断 2026-09-03)

**現在の挙動 (HOME が解決できなければ走査せず即エラー) のままでよい。**

着手前に実装と挙動を確認した (2026-09-03):

| 確認したこと | 結果 |
|---|---|
| 実装 | `os.UserHomeDir()` の失敗で `os.Exit(exitcode.EnvFailure)` (`svc.Scan` を呼ばない) |
| 実挙動 (`env -i PATH=/usr/bin:/bin`) | rc=**3** / stdout 空 / stderr `svcdoctor: $HOME is not defined` |

issue 本文の記述どおりで、issue が提案していた「HOME 非依存の 2 ディレクトリだけでも走査を続ける」
方向へは**変えない**。HOME も引けない環境で出した部分的な診断を「その環境の全体像」と
読まれる方が危ない、という判断。

🚨 **diskdoctor との非対称は残るが、これも意図的**。あちらはカタログの大半が HOME 非依存なので、
HOME を要るエントリだけ弾いても残る診断の情報量が大きい (issue 175)。svcdoctor は 3 つ中 1 つが
HOME 依存で、性質が違う。

### 再評価の条件

HOME 非依存の 2 ディレクトリ (`/Library/LaunchAgents` / `/Library/LaunchDaemons`) だけでも
診断を残したくなったとき。そのときは元の対応案どおり、失敗を `svc.Options` へ渡して
該当ディレクトリだけ `DirErrs` に落とす (rc は 2 になり diskdoctor と揃う)。

**この判断は `src/doctor/cmd/svcdoctor/main.go` の該当箇所にもコメントで残した**
(issue は移動するがコードは残る。`pending-issue-rationale-in-code.md`)。
次の監査が同じ指摘を再生成したら、そのコメントで即棄却できる。
