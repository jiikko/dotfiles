# macOS のヘルスチェック項目 (glogx `H` 画面の情報源)

時点: **2026-09-27、macOS 27.0 (26A428) / Apple Silicon** で実測。OS を上げたら「出力の契約」の列を測り直す。

glogx に `H` でヘルスチェックの結果を一覧する画面を足す予定 (未実装)。この文書はその**前段の情報源**で、
何を見るか・どのコマンドで取るか・どう判定するか・出力がどの stream に出るかを、実装より先に固定する。
画面の仕様 (レイアウト・キー) はまだ決めていない (末尾「未決」)。

## 何を「健康」と呼ぶか

次の問いに答えられれば健康とする。番号は下の「項目」の節の番号と同じ。

1. 落ちていないか (カーネルパニック・クラッシュ・メモリ不足による強制終了)
2. 落ちる前兆が無いか (カーネルのメモリリーク・資源の逼迫)
3. ハードウェアが傷んでいないか (SSD・バッテリー・温度)
4. 守りが効いているか (SIP / Gatekeeper / FileVault / XProtect / 自動更新)
5. 失っても戻せるか (バックアップ)
6. 常駐が壊れていないか (Apple 以外のカーネル拡張・異常終了した常駐)
7. 推奨の設定になっているか (ログインと共有の設定・開発環境の作り。1〜6 と違い、外れていても壊れてはいない)

**unified log の error / fault の行数は健康の指標にしない。** 平常でも Apple のコンポーネントが
30 分で 20 万〜34 万行を error / fault で出す (実測: アップグレード直後 339,479 行、4 時間後 206,442 行)。
想定どおりの動作 (「ファイルが無いので既定値」「権限が無いので読まない」) も error で出るので、
数が減っても増えても何も言えない。ログは**名指しした事象を狙って引くとき**だけ使う (8 節)。

## 判定の語彙

| 値 | 意味 |
|---|---|
| ok | 問題なし |
| 注意 | 今は壊れていないが、放置すると困る / 設定が推奨から外れている |
| 異常 | 対処が要る |
| **判定不能** | コマンドが無い・権限が無い・timeout・出力が読めない。**ok に丸めない** (`src/doctor/README.md` の「検査できなかったを緑にしない」と同じ) |

## スコアリング (予定)

`H` の画面では、項目の一覧に加えて**点数**を出したい。一目で「前より良くなったか / 悪くなったか」が分かるようにするため。
重みと計算式はまだ決めていない。先に決めておく原則は次のとおり。

- **点数を 2 つに分ける**: 「健康」(1〜6 節。壊れていないか) と「設定」(7 節。推奨の設定になっているか)。
  1 つに混ぜると、設定の点が高いせいでカーネルパニックのような重い異常が目立たなくなる
- **異常が 1 件でもあれば、点数に関係なく赤く出す**。点数は平均なので、重い 1 件を薄める。点数は異常を隠す道具にしない
- **判定不能は点数の計算に入れない** (分母から外す)。そのうえで「判定不能 N 件」を点数の横に必ず出す。
  判定不能を満点扱いにすると、検査できないほど点が上がってしまう (「検査できなかったを緑にしない」と同じ理由)
- **減点の理由をたどれるようにする**: 点数から、どの項目が何点下げたかを開けるようにする。点数だけでは何を直せばよいか分からない
- **重みは項目ごとに持つ**: 同じ「注意」でも、Time Machine の未設定と SSH の鍵が RSA なのとでは重さが違う。
  重みは項目の定義と同じ場所に置く (別の表に分けると、項目を足したときに重みを付け忘れる)

未決:

- 重みの値と、点数の計算式 (100 点からの減点にするか、項目ごとの加点の割合にするか)
- 前回の点数を残して比べるか。残すなら、何をどこに保存するか (glogx の既存のキャッシュの置き場所に合わせる)
- 判定不能が多いとき (例えば半分を超えたとき) に、点数そのものを出さないか

## 項目

「取得」はすべて **sudo なし**で動くことを確認済み。sudo が要るものは下の「対象外」へ分けた。
所要はこの Mac での 1 回の実測 (負荷で変わる。load average 3〜30 の間で測った)。

### 1. 落ちていないか

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| カーネルパニック | `/Library/Logs/DiagnosticReports/*.panic` の件数と最新の日時 | 直近 N 日に 1 件でも = 異常 | 🚨 **`.contents.panic` という隠しファイルが同じ場所にある**。同じパニックを Apple に送るための付随データ (中身の `log_path` が本物の `panic-full-<日時>.panic` を指し、同じ `panic_string` を持つ)。数えると 1 回のパニックが 2 件になるので、ドットで始まる名前を除く |
| パニックの原因 | 上の `.panic` の 2 行目以降の JSON の `panicString` の 1 行目 | 表示だけ | 1 行目がヘッダの JSON (`bug_type` 210 = パニック / `os_version`)、2 行目以降が本体の JSON。`zone map exhausted ... [data.kalloc.1024]` なら下の「カーネルのメモリリーク」と同じ事象 (issue 500) |
| クラッシュ | `~/Library/Logs/DiagnosticReports/*.ips` と `/Library/Logs/DiagnosticReports/*.ips` の、起動後の件数をプロセス名で集計 | 同じプロセスが短時間に繰り返す = 注意 | ファイル名は `<プロセス名>-<日時>.ips`。`.ips` も 2 部構成の JSON (ヘッダ + 本体。本体の `procPath` / `parentProc` / `exception.signal` / `procLaunch` と `captureTime` の差 = 起動からの寿命) |
| メモリ不足の強制終了 | `/Library/Logs/DiagnosticReports/JetsamEvent-*.ips` | 表示だけ (件数と、そのとき一番大きかったプロセス) | 本体の `processes[]` の `rpages` × `pageSize` が各プロセスの常駐量。実測の 2 件では、`reason` の欄があるプロセスが殺された側だった |

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
| カーネルのメモリリーク | `zprint` の `data.kalloc.1024` の行の 7 列目 (inuse) | 1,500 万以上 = 異常 (再起動を促す)。時間あたり数万個のペースで増え続ける = 注意 | rc=0 / stdout 約 400 行 / 0.1 秒。sudo なしだと size の列は 0K になるが、**inuse の個数は読める**。平常は数千 (起動 4 時間で 2,346) |
| メモリの逼迫 | `memory_pressure` の最終行 `System-wide memory free percentage: N%` | 閾値は未決 | rc=0 / stdout 28 行 / 0.02 秒 |
| スワップ | `sysctl -n vm.swapusage` の `used` | 0 でない = 表示 (単独では判定しない) | rc=0 / stdout 1 行 |
| ディスクの空き | `df -k /System/Volumes/Data` | 閾値は未決 (内訳は `D` の doctor が持つ) | 🚨 `/` ではなく **Data ボリューム**を見る (`/` はシステムの封印されたボリュームで、実測では 926G 中 13G しか使っていなかった) |

load average は判定に使わない。テストや Spotlight の再インデックスで 30〜47 まで上がるが、それ自体は故障ではない。

`data.kalloc.1024` の閾値の出典は issue 500 (done)。macOS 15.7.7 で、この zone に約 2,120 万個 (20GB) が溜まって
`zone map exhausted` でパニックした (起動から 83 日目)。上流にも同じ zone・同じ上限の報告がある
(Claude Code が動いている macOS 15 / 26)。macOS 27 に上げた後は漏れが止まっているが、原因は特定していないので、
この項目は**再発の検知**のために置く (issue 500 の「再開のきっかけ」と同じ条件)。

### 3. ハードウェア

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| SSD | `diskutil info disk0` の `SMART Status:` | `Verified` = ok、それ以外 = 異常 | rc=0 / stdout 29 行 / 0.1 秒 |
| バッテリー | `system_profiler SPPowerDataType` の `Condition:` / `Maximum Capacity:` / `Cycle Count:` | `Condition` が `Normal` 以外 = 注意。バッテリーの無い Mac (デスクトップ) では対象外 | rc=0 / stdout 約 60 行 / 0.2 秒。🚨 **シリアル番号が含まれる**ので、出力をそのまま画面やログに流さず、必要な 3 行だけ抜く |
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
| 保留中のアップデート | `softwareupdate --list` | 保留あり = 注意 | 🚨 **遅い (4.7 秒)**。🚨 「保留なし」の 1 行 `No new software available.` は **stderr** に出る (stdout は見出しの 3 行だけ)。保留なしのときは rc=0。**保留があるときの出力と rc は未実測** (その状態に居なかったため) |

### 5. バックアップ

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| Time Machine の設定 | `tmutil destinationinfo` | `No destinations configured.` = 注意 | rc=0 / **stdout** 1 行 |
| 最後のバックアップ | `tmutil latestbackup` | 設定があって最新が古い = 注意 | 🚨 **失敗しても rc=0**。宛先が無いときは stdout が空で、stderr に `Failed to mount backup destination ...` が出る。rc では成否が分からないので、stdout にバックアップのパスが出るかで見る。**バックアップがあるときの出力の形は未実測** (この Mac に宛先が無いため) |
| ローカルスナップショット | `tmutil listlocalsnapshots /System/Volumes/Data` | 表示だけ | rc=0。0 件でも見出しの 1 行は出る |

### 6. 起動・常駐

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| Apple 以外のカーネル拡張 | `kmutil showloaded --list-only` から `com.apple` 以外 | あれば表示 | rc=0 / stdout 約 270 行 / 0.3 秒。stderr に `No variant specified, falling back to release` が毎回 1 行出る (異常ではない) |
| 異常終了した常駐 | `launchctl list` の Status 列 (0 と `-` 以外) | Apple 以外で非 0 = 注意 | rc=0 / stdout 約 550 行。**壊れた登録の診断は `bin/svcdoctor` が正本** (下の「`D` との境界」) |

### 7. ベストプラクティスが適用されているか (設定の検査)

1〜6 は「今、壊れていないか」を見る。この節は「推奨の設定になっているか」を見る。壊れてはいないので、外れていても**注意**止まりにする。
出典は CIS の macOS ベンチマーク (自動ログイン・画面ロック・ファイアウォール・リモートログイン・ゲスト) と、
Apple Silicon での開発環境の定番 (Homebrew の置き場所・Brewfile・CLT)。どれも sudo なしで取れる。

#### ログインと共有 (CIS 由来)

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| 自動ログイン | `defaults read /Library/Preferences/com.apple.loginwindow autoLoginUser` | キーが無い = ok | 🚨 **ok のときに rc=1** (stderr に `Could not find key 'autoLoginUser'`)。有効なときは rc=0 でユーザー名が出るはず (未実測) |
| ゲストユーザー | `defaults read /Library/Preferences/com.apple.loginwindow GuestEnabled` | `0` = ok | rc=0 / stdout 1 行 |
| 画面ロックまでの猶予 | `sysadminctl -screenLock status` | `immediate` = ok | 🚨 **結果は stderr** に出る (`... screenLock delay is immediate`。先頭に日時と pid が付く)。stdout は空、rc=0 |
| スクリーンセーバが始まるまで | `defaults -currentHost read com.apple.screensaver idleTime` (秒) | 閾値は未決 (CIS の推奨値は確かめていない) | rc=0 / stdout 1 行 (実測 600) |
| リモートログイン (SSH) | `launchctl print system/com.openssh.sshd` | サービスが無い = ok | 無効のときは **rc=113** で stderr に `Could not find service` (有効なときの出力は未実測)。念のため `nc -z -G 1 127.0.0.1 22` の rc=1 (つながらない) も併せて見る |
| ファイル共有 (SMB) | `launchctl print system/com.apple.smbd` | サービスが無い = ok | 無効のときは rc=113 (上と同じ形) |
| 画面共有 | `launchctl print system/com.apple.screensharing` | 🚨 **判定の式が未確定** | 無効にしてあっても rc=0 でサービスの定義が出る (`active count = 0`)。サービスがあるかどうかでは有効・無効を区別できない。有効にした状態と比べて、違いの出る欄を決める |
| ファイアウォールのステルスモード | `/usr/libexec/ApplicationFirewall/socketfilterfw --getstealthmode` | off = 注意 (ファイアウォール自体と合わせて見る) | rc=0 / stdout 1 行 (`Firewall stealth mode is off`) |
| sudo を Touch ID で通す | `/etc/pam.d/sudo_local` に `pam_tid.so` の行があるか | 無い = 注意 (必須ではない。パスワードの手打ちを減らせる) | ファイルが無ければ未設定。ひな形 `/etc/pam.d/sudo_local.template` が OS に入っている。**`sudo` の本体ではなく `sudo_local` に書く** (本体は OS の更新で上書きされる) |

#### 開発環境

| 項目 | 取得 | 判定 | 出力の契約 (実測) |
|---|---|---|---|
| Homebrew の置き場所 | `/opt/homebrew/bin/brew --prefix` | `/opt/homebrew` = ok | Apple Silicon の標準の置き場所 |
| Intel 版の Homebrew の残り | `/usr/local/bin/brew` があるか | ある = 注意 (2 つの brew が同居すると PATH の順で別の版を掴む) | 無ければ `ls` が rc=1 |
| Rosetta | `arch -x86_64 /usr/bin/true` | 入っていない = ok。入っている = 注意 (Intel 用のバイナリが動いてしまうので、Apple Silicon 版への移行漏れに気づけない) | 入っていないときは rc=1 で stderr に `Bad CPU type in executable`。入っているときの出力は未実測 (この Mac に Rosetta が無いため。rc=0 になるはず) |
| Command Line Tools | `pkgutil --pkg-info=com.apple.pkg.CLTools_Executables` の `version:` と `xcode-select -p` | CLT のメジャー版 = OS のメジャー版 = ok。古い = 注意 (brew のソースビルドや native gem のビルドが失敗しうる) | Xcode が無いなら `xcode-select -p` は `/Library/Developer/CommandLineTools` |
| Brewfile との一致 | `HOMEBREW_NO_AUTO_UPDATE=1 brew bundle check --file <dotfiles>/Brewfile` | rc=1 (足りない) = 注意 | 🚨 **`HOMEBREW_NO_AUTO_UPDATE=1` を付ける**。付けないと、検査のたびに brew の自動更新 (ネットワーク) が走る。入っているのに Brewfile に無いものは `brew bundle cleanup --file ...` が一覧する (引数なしは dry-run で、消さない)。実測では、`brew leaves --installed-on-request` の 72 個に対して Brewfile の `brew` の行は 19 行で、cask もほとんど載っていなかった = **Brewfile からこのマシンを作り直せない** |
| brew の依存情報の傷み | `brew bundle` などが stderr に出す `found a circular dependency` | 出る = 注意 | 実測: `libtiff, webp` で出た。keg の記録が古いときに出る。直し方は brew が同じ警告の中で案内する |
| ログインシェル | `dscl . -read /Users/<user> UserShell` | 表示だけ | Homebrew の zsh (`/opt/homebrew/bin/zsh`) にしているなら、`/etc/shells` に載っているかも見る (載っていないとログインできない)。brew を壊すとログインシェルごと起動しなくなる点に注意 |
| バージョン管理ツールの二重化 | rbenv / nodenv / goenv / pyenv を `whence -p` で解決し、`~/.rbenv` などの単体のディレクトリと `~/.anyenv/envs/*` の両方があるかを見る | 同じ言語に 2 系統ある = 注意 | 実測: ruby と node は Homebrew の rbenv / nodenv (`~/.rbenv` / `~/.nodenv`)、go は anyenv の goenv。**`~/.goenv` と `~/.pyenv` の単体のディレクトリも残っていて**、どちらも PATH から使われていない (残骸の可能性。未確認) |
| git の認証情報 | `git config --show-origin --get-all credential.helper` | `osxkeychain` = ok | Homebrew の git なら `/opt/homebrew/etc/gitconfig` に設定がある (`~/.gitconfig` には無い)。global だけを見ると「未設定」に見える |
| SSH の鍵 | `~/.ssh/*.pub` を `ssh-keygen -lf` で読む | RSA だけ = 注意 (ed25519 を推奨) | 判定には鍵の種類 (出力の末尾の括弧) だけを使う。コメント欄にアカウント名が入っていることがあるので、行をそのまま表示しない |

### 8. ログ (狙って引くときだけ)

- **`/usr/bin/log` を絶対パスで呼ぶ**。zsh には `log` という組み込みコマンドがあり、素の `log show` は
  `too many arguments` で失敗する (実測)
- 絞り込まない `log show --last 30m` は数十万行になる (実測 20 万〜34 万行)。
  predicate で名指しすれば速い: `--last 1h --predicate 'process == "kernel" AND eventMessage CONTAINS "zone map"'` は 2 秒 (該当 0 件)
- 使いどき: パニックの前兆 (issue 500 では、パニックの 3 時間前から zone map の枯渇を理由に jetsam がアイドルのプロセスを殺し始めていた)

## `D` (doctor) との境界

glogx の `D` はすでに、ディスクの掃除候補・壊れた launchd 登録・Homebrew の警告・Docker の未使用資源を診断し、
削除まで持っている (`src/doctor/README.md`)。`H` はこれを**作り直さない**。

- 容量の内訳・掃除 → `D` (ディスクタブ)。`H` は「Data ボリュームの空き」の 1 行だけを出す
- 壊れた常駐 → `bin/svcdoctor` (`-json` と終了コード `0` / `1` / `2` / `3` の契約がある)。`H` は終了コードを 1 行に要約するか、`D` へ誘導する
- Homebrew の `brew doctor` → `D` の Homebrew タブ
- **`H` はシステムの状態を変えない**。何かを消したり設定を変えたりする経路を持たない (直し方はコマンドを見せるだけ)。点数の履歴を残す場合も、書くのは glogx 自身のキャッシュだけ

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
- [CIS Apple macOS Benchmarks](https://www.cisecurity.org/benchmark/apple_os) — 7 節の「ログインと共有」の出典 (推奨値そのものは未確認。本文はベンチマークが挙げる項目の種類だけを使った)
