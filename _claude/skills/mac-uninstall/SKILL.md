---
name: mac-uninstall
version: 1.0.0
description: 使っていないアプリ (brew の cask・App Store・手で入れたもの) と brew の formula を、残骸 (キャッシュ・ログ・設定・Containers) ごと安全に片付ける。候補を最後に使った日の古い順に見せて選ばせ、消える物をパスと大きさで並べた計画を見せて承認を取ってから、bin/mac-uninstall で brew uninstall かゴミ箱への移動で片付ける (rm はしない)。設定とデータはアプリごとに選んだときだけ消す。「使っていないアプリを消して」「アプリをアンインストールして」「brew で入れたものを整理して」「アプリを残骸ごと消して」「mac-uninstall」で発火。一時ファイルの掃除は cleanup-tmp-dir、Mac 全体の診断は mac-healthcheck。
---

# mac-uninstall — 使っていないアプリと formula を残骸ごと片付ける

判定と実行は **`bin/mac-uninstall` だけが行う** (本体は `src/doctor/uninstall/`)。片付け方は 2 通りだけ:
brew の管理下 (cask・formula) は `brew uninstall`、それ以外 (App Store・手で入れたアプリと残骸) はゴミ箱へ移す (Finder から戻せる)。

🚨 **自分で `rm` / `mv` / `brew uninstall` を打たない**。`mac-uninstall apply` は、承認された計画の ID を照合し、その時点の候補を作り直して、
計画との交わりのうち実体が変わっていないものだけを動かす。手で打つとこの確かめを全部飛ばす。
🚨 **`brew uninstall --zap`・`brew autoremove`・`brew cleanup` を承認なしに打たない** (計画に載っていないものを消す)。

## 片付けないもの (計画に「手で確かめるもの」として載るだけ)

- **動いているアプリ・formula**: 計画を作らずに断る。formula は、プログラムかライブラリをどれかのプロセスが使っていれば「動いている」
  (`lsof` の txt で見る。zsh・tmux・nvim・mysqld などは常に当たる)。終了してから作り直す (終了は人に頼む。未保存の作業を失うのは人なので)。
  `lsof` / `ps` が失敗して確かめられないときも計画を作らない
- **ログインシェルと `/etc/shells` のシェル**の formula: 消さない
- **本体の外に何かを置いたアプリ**: pkg のインストーラで入れたもの (Karabiner など)、同じ開発元の root の常駐 (`/Library` の LaunchDaemons など。zoom・Docker) か
  pkg の受領があるもの、cask の uninstall の手順に sudo が要りうるもの (pkgutil・script・ホームの外の delete) がある cask。本体も残骸も動かさず、
  付属のアンインストーラ・開発元の手順・ターミナルで打つ `brew uninstall --cask <token>` を案内する
- **名前で一致した Application Support**・**Group Containers** (同じ開発元の他のアプリと共有されうる)・**LaunchAgents** (読み込まれたままのことがある)・
  **Apple の dir の中** (`com.apple.*`・`CloudDocs`)・**ホームの外** (sudo が要る)
- **Apple のアプリ** (bundle ID が `com.apple`): 候補にも出さない。**同じ bundle ID のアプリが 2 つ**あるときも計画を作らない

## 手順

1. **置き場を決める**: `skill-tmpdir mac-uninstall` が返す絶対パスを `<D>` とし、以降はリテラルで書く
2. **候補を見る**: `mac-uninstall list -json > <D>/list.json 2> <D>/list.err; echo $? > <D>/list.rc` と、表の形 `mac-uninstall list > <D>/list.md 2>&1`
   (読み取りだけ。数十秒)。rc=2 は一覧の一部が取れなかったもの (Notes) があるだけで、取れた分は使える。
   チャットには、最後に使った日が古いアプリ (上位 10) と、大きいアプリ、formula の数を短く出し、`<D>/list.md` の絶対パスを示す。
   🚨 **「最後に使った日」が不明 = 使っていない、ではない** (cask の更新で消える・`/Applications` の外では記録されない)。不明のものは不明と書く
3. **選ばせる**: 片付けられない候補は選択肢に出さない: アプリは `manual` か `running` が立っているもの、formula は `protected` か
   `running` が立っているもの (どれも計画が断る)。それらは `<D>/list.md` の「片付けられない理由」の欄で見せるだけにする。
   AskUserQuestion (`multiSelect: true`) で、アプリの候補を 4 つずつ聞く (1 回に 4 問まで)。各選択肢の説明に
   入り方・大きさ・最後に使った日を書く。formula は数が多いので、人に名前で挙げてもらうか、「自分で入れた」(`on_request`) で入れた日が古いものを
   候補にして聞く。🚨 formula の「使っていない」は判定する手がかりが無い (入れた日と、他から依存されていないことしか分からない) と添える。
   🚨 `used_by` が付いた formula (コマンドの名前がスクリプト・hook・設定に出てくる。例: dotfiles の hook が呼ぶ jq) は、選ばれたら
   その場所を見せて、本当に消してよいかを 1 つずつ別に聞く。消すとそのスクリプトが壊れる。名前が一般的な語 (git・go) だと
   関係の無い一致もあるので、場所の中身を grep して見せる
   続けて、選んだアプリごとに「設定とデータも消すか」を聞く (既定は残す)。説明に、消すと失うもの (ログイン状態・設定・アプリの中のデータ) を書く
4. **計画を作る**: `mac-uninstall plan -out <D> -app <bundle ID,...> -data <bundle ID,...> -formula <名前,...> > <D>/plan.md 2> <D>/plan.err; echo $?`
   (何も動かさない)。`plan.err` の `plan:` と `id:` の行に計画のパスと ID が出る。rc=2 なら `plan.err` の理由 (動いている・見つからない) を伝え、
   直してから作り直す
5. **見せて承認を取る**: `<D>/plan.md` の表を見せ、AskUserQuestion で「この計画 (ID) を実行してよいか」を聞く。選択肢は「実行する」と「実行しない」。
   「実行する」の説明に次を書く:
   - 消えるもの: アプリ・formula の名前と、段ごとの件数と合計の大きさ (本体 / 作り直されるもの / 設定とデータ)
   - 戻し方: ゴミ箱へ移したものは Finder から戻せる (容量はゴミ箱を空にするまで減らない)。brew が消したものは `brew install` で入れ直す
     (cask の設定とデータを残していれば元の状態に戻る)
   - 残るもの: 「手で確かめる」の行 (Group Containers・LaunchAgents・pkg のアプリ) と、その理由
   - 🚨 メモに「使われているかもしれない」が付いた formula: その場所と、消すと壊れうること (手順 3 で確かめた答えも添える)
   回答が無い・「実行しない」なら何もせずに終える。選び直したいと言われたら手順 3 からやり直す (計画のファイルを手で書き換えない)
6. **実行する**: `mac-uninstall apply -plan <計画のパス> -approve <ID> -json > <D>/apply.json 2> <D>/apply.err; echo $? > <D>/apply.rc`。
   計画は作ってから 1 時間で使えなくなる (rc=2 で「計画が古い」なら手順 4 から作り直し、新しい ID で承認を取り直す)。
   rc=0 = 全部片付けた / 1 = 止めた・失敗したものがある (`skipped` / `failed` の理由を読む) / 2 = 計画を使えない / 3 = 実行環境の失敗
7. **後片付けを確かめる** (どれも読み取りか、別の承認を取ってから):
   - `svcdoctor > <D>/svcdoctor.txt 2>&1`: 消したアプリの常駐が壊れた登録として残っていないか
   - formula を消したなら `brew autoremove --dry-run`: 依存として入っていて要らなくなったもの。消すなら一覧を見せて**別の承認**を取ってから `brew autoremove`
   - `brew cleanup --dry-run`: 古い版とダウンロードの残り。消すなら同じく別の承認を取る
   - 「手で確かめる」の行は、手順を 1 行ずつ示す (LaunchAgents なら `launchctl bootout gui/$(id -u) <plist>` の後にゴミ箱へ、など)
8. **報告する**: 片付けたもの (件数と大きさ)・止めたものと理由・ゴミ箱の移し先 (`apply.json` の `trash`)・手で確かめるものを出す

## 読み方の注意

- **`Containers` の大きさが「不明」**: macOS は他のアプリの Containers を読ませない (プライバシーの保護)。ゴミ箱へ移すのも断られることがあり、
  そのときは `failed` に出る。システム設定 > プライバシーとセキュリティ > フルディスクアクセス で端末に許可するか、Finder で手で消す
- **名前で一致した Application Support** (`by_name`): bundle ID ではなくアプリの名前の dir。動かさずに見せるだけなので、同名の別物でないかを中身で確かめ、
  要らなければ人が Finder で消す
- **formula の「使われているかもしれない」の範囲**: 入口は 3 つ。①シェル・tmux の設定から `source` / `.` / `source-file` / `run-shell` の
  引数に出たパスを 2 段まで (絶対パスと `${VAR:-既定}` の既定も辿る)、②`~/.claude/settings.json` の `command` と、そこから呼ぶスクリプトの
  本文に出る `~` / `$HOME` / 自分の置き場 (`$(dirname "$0")` など) からのパスを 2 段まで、③`~/.config` の下 (直下の symlink の dir も辿る。
  道具ごとに 3 段目・300 ファイル・1 ファイル 1MB まで)。`~/Library`・履歴 (`*_history`)・補完のキャッシュ・バイナリは読まない。
  読む量は合計 32MB まで (超えたら一覧の判定不能に出る)。当たり方はコマンドの名前 (`bin` の中身) が語として出るか、
  `Cellar/` `opt/` `share/` `plugins/` の直後に formula の名前が出るか。それ以外 (cron・repo の Makefile やスクリプト・nvim が runtimepath
  から読む Lua・`libexec/bin` のコマンド・`$ZDOTDIR` などの変数で指す先・`$here/lib` のような変数経由の自分の置き場) から呼ぶものは見ていない
- **formula の「動いている」は広い**: ライブラリ (openssl・icu4c など) を使うプロセスがあれば当たる。`brew leaves` に出るのは他の formula から依存されていない
  ものだけで、それでもアプリやスクリプトが直接使っていることがある
- **同じ bundle ID のコピー**: 開発中のビルドや Docker の更新の途中のコピーがあると、計画を作らずにそのパスを出す。消してよいコピーなら人が片付けてから
  作り直す。🚨 Spotlight の索引の外 (ドットで始まる dir・`.noindex`・索引しない外付け) にある**動いていない**コピーは検出しない
- **cask の `uninstall` の手順**: `brew uninstall --cask` は cask の定義に従ってアプリを終了させたり常駐を止めたりすることがある。sudo を求められたら止めて人に渡す
