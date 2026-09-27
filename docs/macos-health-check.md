# macOS のヘルスチェック項目 (glogx `H` 画面の情報源)

時点: **2026-09-27、macOS 27.0 (26A428) / Apple Silicon** で実測。OS を上げたら「出力の契約」の列を測り直す。

glogx に `H` でヘルスチェックの結果を一覧する画面を足す予定 (未実装)。この文書はその**前段の情報源**で、
何を見るか・どのコマンドで取るか・どう判定するか・出力がどの stream に出るかを、実装より先に固定する。
画面の仕様 (レイアウト・キー) はまだ決めていない (末尾「未決」)。

## 何を「健康」と呼ぶか

OS のアップグレード直後に 1 つずつ確かめた順に、次の問いに答えられれば健康とする。

1. 落ちていないか (カーネルパニック・クラッシュ・メモリ不足による強制終了)
2. 落ちる前兆が無いか (カーネルのメモリリーク・資源の逼迫)
3. ハードウェアが傷んでいないか (SSD・バッテリー・温度)
4. 守りが効いているか (SIP / Gatekeeper / FileVault / XProtect / 自動更新)
5. 失っても戻せるか (バックアップ)

**unified log の error / fault の行数は健康の指標にしない。** 平常でも Apple のコンポーネントが
30 分で 20 万〜34 万行を error / fault で出す (実測: アップグレード直後 339,479 行、4 時間後 206,442 行)。
想定どおりの動作 (「ファイルが無いので既定値」「権限が無いので読まない」) も error で出るので、
数が減っても増えても何も言えない。ログは**名指しした事象を狙って引くとき**だけ使う (下の「ログ」行)。

## 判定の語彙

| 値 | 意味 |
|---|---|
| ok | 問題なし |
| 注意 | 今は壊れていないが、放置すると困る / 設定が推奨から外れている |
| 異常 | 対処が要る |
| **判定不能** | コマンドが無い・権限が無い・timeout・出力が読めない。**ok に丸めない** (`src/doctor/README.md` の「検査できなかったを緑にしない」と同じ) |

## 項目

「取得」はすべて **sudo なし**で動くことを確認済み。sudo が要るものは下の「対象外」へ分けた。
所要はこの Mac での 1 回の実測 (負荷で変わる。load average 3〜30 の間で測った)。

### 1. 落ちていないか

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| カーネルパニック | `/Library/Logs/DiagnosticReports/*.panic` の件数と最新の日時 | 直近 N 日に 1 件でも = 異常 | 🚨 **`.contents.panic` という隠しファイルが同じ場所にある**。これはパニックではないので、ドットで始まる名前を除く。本物は `panic-full-<日時>.panic` |
| パニックの原因 | 上の `.panic` の 2 行目以降の JSON の `panicString` の 1 行目 | 表示だけ | 1 行目がヘッダの JSON (`bug_type` 210 = パニック / `os_version`)、2 行目以降が本体の JSON。`zone map exhausted ... [data.kalloc.1024]` なら下の「カーネルのメモリリーク」と同じ事象 (issue 500) |
| クラッシュ | `~/Library/Logs/DiagnosticReports/*.ips` と `/Library/Logs/DiagnosticReports/*.ips` の、起動後の件数をプロセス名で集計 | 同じプロセスが短時間に繰り返す = 注意 | ファイル名は `<プロセス名>-<日時>.ips`。`.ips` も 2 部構成の JSON (ヘッダ + 本体。本体の `procPath` / `parentProc` / `exception.signal` / `procLaunch` と `captureTime` の差 = 起動からの寿命) |
| メモリ不足の強制終了 | `/Library/Logs/DiagnosticReports/JetsamEvent-*.ips` | 表示だけ (件数と、そのとき一番大きかったプロセス) | 本体の `processes[]` の `rpages` × `pageSize` が各プロセスの常駐量。`reason` があるものが殺された側 |

クラッシュの件数を異常に数えない方がよいもの (2026-09-27 に実物を見て、どれも無害だった):

- **`bash-*.ips` が大量**: dotfiles の Go のテストが `sh -c 'kill -SEGV $$'` を実行するたびに 1 件出る
  (`src/pro-con/dispatcher/execrunner_test.go` の `TestRunCommandKeepsMeaningAndExitCode`。1 回走らせて 1 件増えるのを確認)。
  macOS の `/bin/sh` の実体は `/bin/bash` なので名前が bash になる。形は「寿命 3ms・親は終了済み・`main` から直接 `__kill`」
- **`ExcUserFault_<アプリ>-*.ips`**: 動いているアプリを Homebrew の cask が上書きしたとき (`EXC_GUARD`・落ちない記録)。
  アプリを再起動すれば止まる。Chrome はこの状態で新しいタブが開けなくなる (補助プロセスの旧版のファイルが消えるため)
- **`WebThumbnailExtension-*.ips`**: Finder / Spotlight の HTML のサムネイル生成が WebKit の assert で落ちる。OS 側の不具合
- **`tmux-*.ips` (寿命 約 3 秒・親は launchd)**: tmux 3.7b のサーバが pane を閉じるときのレイアウト処理 (`layout_close_pane`) で落ちる。
  普段使いのサーバではなく短命のサーバで起きていた。どこが立てたサーバかは未特定

### 2. 落ちる前兆

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| カーネルのメモリリーク | `zprint` の `data.kalloc.1024` の行の 7 列目 (inuse) | 1,500 万 = 異常 (再起動を促す)。時間あたり数万個のペースで増え続ける = 注意 | rc=0 / stdout 約 400 行 / 0.1 秒。sudo なしだと size の列は 0K になるが、**inuse の個数は読める**。平常は数千 (起動 4 時間で 2,346) |
| メモリの逼迫 | `memory_pressure` の最終行 `System-wide memory free percentage: N%` | 閾値は未決 | rc=0 / stdout 28 行 / 0.02 秒 |
| スワップ | `sysctl -n vm.swapusage` の `used` | 0 でない = 表示 (単独では判定しない) | rc=0 / stdout 1 行 |
| ディスクの空き | `df -k /System/Volumes/Data` | 閾値は未決 (内訳は `D` の doctor が持つ) | 🚨 `/` ではなく **Data ボリューム**を見る (`/` はシステムの封印されたボリュームで常に数 % しか使っていない) |

load average は判定に使わない。テストや Spotlight の再インデックスで 30〜47 まで上がるが、それ自体は故障ではない。

`data.kalloc.1024` の閾値の出典は issue 500 (done)。macOS 15.7.7 で、この zone に約 2,120 万個 (20GB) が溜まって
`zone map exhausted` でパニックした (起動から 83 日目)。上流にも同じ zone・同じ上限の報告がある
(Claude Code が動いている macOS 15 / 26)。macOS 27 に上げた後は漏れが止まっているが、原因は特定していないので、
この項目は**再発の検知**のために置く (issue 500 の「再開のきっかけ」と同じ条件)。

### 3. ハードウェア

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| SSD | `diskutil info disk0` の `SMART Status:` | `Verified` = ok、それ以外 = 異常 | rc=0 / stdout 29 行 / 0.1 秒 |
| バッテリー | `system_profiler SPPowerDataType` の `Condition:` / `Maximum Capacity:` / `Cycle Count:` | `Condition` が `Normal` 以外 = 注意 | rc=0 / stdout 約 60 行 / 0.2 秒。🚨 **シリアル番号が含まれる**ので、出力をそのまま画面やログに流さず、必要な 3 行だけ抜く |
| 温度 | `pmset -g therm` | `No thermal warning level has been recorded` と `No performance warning level has been recorded` の両方がある = ok | rc=0 / stdout 3 行 |
| スリープ／復帰の失敗 | `pmset -g log` | 🚨 **判定の式が未較正**。失敗の行が 1 件も無い状態でしか確かめていないので、`Failure` を含む行を数える式が本当に失敗を拾うかは分からない | rc=0 / stdout 約 3,600 行 / 0.2 秒 |

### 4. 守り

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| SIP | `csrutil status` | `enabled` = ok | rc=0 / stdout 1 行 |
| Gatekeeper | `spctl --status` | `assessments enabled` = ok | rc=0 / stdout 1 行 |
| FileVault | `fdesetup status` | `FileVault is On.` = ok | rc=0 / stdout 1 行 |
| ファイアウォール | `/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate` | 無効 = 注意 | rc=0 / stdout 1 行 (`Firewall is disabled. (State = 0)`) |
| XProtect の定義 | `defaults read /Library/Apple/System/Library/CoreServices/XProtect.bundle/Contents/Info CFBundleShortVersionString` | 表示だけ (最新かどうかを知る手段が手元に無い) | 🚨 **`xprotect version` を使わない**。iCloud 経由で最後に取った版の記録を返し、実際に入っている版と食い違う (実測: `xprotect version` は 5331、バンドルは 5360) |
| 自動更新の設定 | `defaults read /Library/Preferences/com.apple.SoftwareUpdate` の `CriticalUpdateInstall` / `ConfigDataInstall` / `AutomaticDownload` | どれか 0 = 注意 | `LastSuccessfulDate` (最後に更新の確認が成功した日時) も同じ出力にある |
| 保留中のアップデート | `softwareupdate --list` | 保留あり = 注意 | 🚨 **遅い (4.7 秒)**。🚨 「保留なし」の 1 行 `No new software available.` は **stderr** に出る (stdout は見出しの 3 行だけ)。rc は保留の有無によらず 0 |

### 5. バックアップ

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| Time Machine の設定 | `tmutil destinationinfo` | `No destinations configured.` = 注意 | rc=0 / **stdout** 1 行 |
| 最後のバックアップ | `tmutil latestbackup` | 設定があって最新が古い = 注意 | 🚨 **失敗しても rc=0**。宛先が無いときは stdout が空で、stderr に `Failed to mount backup destination ...` が出る。rc では成否が分からないので、stdout に日時の形のパスがあるかで見る |
| ローカルスナップショット | `tmutil listlocalsnapshots /System/Volumes/Data` | 表示だけ | rc=0。0 件でも見出しの 1 行は出る |

### 6. 起動・常駐

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| Apple 以外のカーネル拡張 | `kmutil showloaded --list-only` から `com.apple` 以外 | あれば表示 | rc=0 / stdout 約 270 行 / 0.3 秒。stderr に `No variant specified, falling back to release` が毎回 1 行出る (異常ではない) |
| 異常終了した常駐 | `launchctl list` の Status 列 (0 と `-` 以外) | Apple 以外で非 0 = 注意 | rc=0 / stdout 約 550 行。**壊れた登録の診断は `bin/svcdoctor` が正本** (下の「`D` との境界」) |

### 7. ログ (狙って引くときだけ)

- **`/usr/bin/log` を絶対パスで呼ぶ**。zsh には `log` という組み込みコマンドがあり、素の `log show` は
  `too many arguments` で失敗する (実測)
- 絞り込まない `log show --last 30m` は数十万行になり、読むだけで分単位かかる。
  predicate で名指しすれば速い: `--last 1h --predicate 'process == "kernel" AND eventMessage CONTAINS "zone map"'` は 2 秒 (該当 0 件)
- 使いどき: パニックの前兆 (issue 500 では、パニックの 3 時間前から zone map の枯渇を理由に jetsam がアイドルのプロセスを殺し始めていた)

## `D` (doctor) との境界

glogx の `D` はすでに、ディスクの掃除候補・壊れた launchd 登録・Homebrew の警告・Docker の未使用資源を診断し、
削除まで持っている (`src/doctor/README.md`)。`H` はこれを**作り直さない**。

- 容量の内訳・掃除 → `D` (ディスクタブ)。`H` は「Data ボリュームの空き」の 1 行だけを出す
- 壊れた常駐 → `bin/svcdoctor` (`-json` と終了コード `0` / `1` / `2` / `3` の契約がある)。`H` は終了コードを 1 行に要約するか、`D` へ誘導する
- Homebrew の `brew doctor` → `D` の Homebrew タブ
- **`H` は読み取り専用にする**。何かを消したり変えたりする経路を持たない (直し方はコマンドを見せるだけ)

名前の衝突: glogx の `cli_health.go` は **claude / codex CLI のログイン状態**の検査で、OS のヘルスチェックとは別物。
`H` の実装で「health」という名前を使うなら、区別が付く名前にする。

## 対象外 (sudo が要る / 重い)

| 項目 | 理由 |
|---|---|
| ログイン項目・バックグラウンド項目 (`sfltool dumpbtm`) | sudo が要る |
| APFS ボリュームの整合性 (`diskutil verifyVolume /`) | 数分かかり、その間ディスクが重くなる。sudo の要否は未実測 |
| カーネルの zone logging (`zlog=`) | SIP を切って再起動が要る (issue 500) |
| Apple Diagnostics (ハードウェア診断) | 再起動してから起動する |

## 取得で踏んだ罠 (実装で同じものを踏まない)

- **Homebrew のサービスの launchd ラベルが変わった**: 2026-09-27 の brew の更新で `homebrew.mxcl.<名前>` が `sh.brew.<名前>` になった。ラベルを決め打ちしない
- **zsh で `path` を変数名に使わない**: `path` は `PATH` と連動した配列なので、代入すると以降のコマンドが見つからなくなる (実測)
- **保護されたフォルダは `du` で数えられない**: `~/Library/Containers` の Docker などは、中の各フォルダが小さく出るのに合計だけ大きい。読めなかったものは判定不能として出す
- **Docker.raw はスパースファイル**: `ls -l` の見かけ (上限) と実際に使っている量 (`du` / `ls -s`) が 2 倍以上違う (実測 119G と 58G)
- **アプリの「最後に使った日時」(`mdls kMDItemLastUsedDate`) は、cask の upgrade でアプリが差し替わると消える**。未使用の判定に使えない

## 未決

- `H` のキー: 2026-09-27 に `src/glogx/*.go` と `src/glogx/README.md` で未使用を確認した。足す前に [`glogx-ui-guide.md`](glogx-ui-guide.md) の語彙と突き合わせる
- 各項目の閾値 (メモリ・ディスクの空き・「直近 N 日」の N)
- 遅い項目 (`softwareupdate --list` の 4.7 秒) の扱い: 非同期にする / 結果をキャッシュする / 手動の再走査だけにする
- 画面の並び (上の 1〜7 の順で出すか、異常のあるものを先に出すか)。見た目は本体へ入れる前にサンプルで決める
  (`~/.claude/rules/decide-layout-in-sample-renderer-first.md`)

## 関連

- `issues/done/500-bug-macos-kernel-zone-leak-from-tmux-clients.md` — カーネルのメモリリークとパニックの調査。`data.kalloc.1024` の閾値と、再発の条件
- [`src/doctor/README.md`](../src/doctor/README.md) — `D` の doctor と CLI 2 本 (`bin/diskdoctor` / `bin/svcdoctor`)。終了コードの語彙
- [`_claude/rules/measure-external-cli-streams-separately.md`](../_claude/rules/measure-external-cli-streams-separately.md) — 上の「出力の契約」の列の測り方
