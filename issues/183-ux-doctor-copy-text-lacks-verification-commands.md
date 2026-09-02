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

## 対応案

ID / 種別ごとに 1 行足す。体 6 が作った対応表:

| ID / 種別 | 裏取りコマンド |
|---|---|
| Paths 系すべて | `du -sk <各 Item.Path>` |
| simulator-runtimes | `xcrun simctl runtime list -j` |
| coresimulator-orphan | `xcrun simctl list devices -j \| grep <UUID>` (無ければ孤児) |
| orphan-container | `mdfind "kMDItemCFBundleIdentifier == '<id>'"` / `ls /Applications ~/Applications` |
| brew-orphan-state / brew-cleanup-residue | `brew info --json=v2 --installed \| jq '.formulae[].name'` / `brew cleanup --dry-run` |
| versionmanager-orphan-root | `echo $RBENV_ROOT $NODENV_ROOT $GOENV_ROOT` / `rbenv root` |
| chrome-tmp (blocked) | `pgrep -x "Google Chrome Canary"` |
| xctest-* / launchd-tmp (boottime 判定) | `sysctl kern.boottime` + `stat -f %Sm <path>` |
| svc A (実行ファイル不在) | `ls -l <MissingExec>` / `plutil -p <plist> \| grep -A2 Program` |
| svc B (再起動ループ) | `launchctl list \| grep <label>` / `launchctl print gui/$(id -u)/<label>` |
| svc C (brew 孤児) | `brew list --formula \| grep <formula>` |

⚠️ `orphan-container` の `mdfind` は単独の判定材料にしない (issue 148 の「`mdfind` 単独に置かないこと」)。
**裏取り用**として出すのは問題ないが、コピー文にもその注意を 1 行添える。

## 受け入れ条件

- [ ] 各 ID のコピー文に裏取りコマンドが入っている (プレースホルダが実値に置換されている)
- [ ] `mdfind` に「単独の判定材料にしない」注記が付いている
