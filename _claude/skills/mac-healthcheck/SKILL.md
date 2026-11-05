---
name: mac-healthcheck
version: 1.0.0
description: この Mac の健康診断をする。観点 (ハードウェアと寿命・プロセスとメモリ・ネットワーク・ディスク・tmux / nvim / Chrome などよく使うソフト) を選ばせ、bin/mac-healthcheck で読み取りだけの診断を回してレポートを書き、メンテナンスが要るか・買い替えを考える時期か・長く使うために異常な箇所が無いかをまとめてから、おかしい箇所を直していくかを項目ごとに聞く。「Mac の健康診断をして」「macOS のヘルスチェック」「暴走しているプロセスやゾンビが無いか見て」「メモリリークしていないか」「買い替えた方がいいか見て」「mac-healthcheck」で発火。一時領域の掃除だけなら cleanup-tmp-dir、クラッシュログの解析だけなら crash-log-analyzer。直す操作は承認を取ってから 1 件ずつ行う。
---

# mac-healthcheck — この Mac の健康診断

診断は **`bin/mac-healthcheck` だけが行う** (本体は `src/doctor/health/`。読み取りだけで、止める・消す・設定を変える経路を持たない)。
項目・判定・出力の契約の正本は `docs/macos-health-check.md`、tmux / nvim / Chrome への助言と寿命の判断の基準は同じ dir の [`advice.md`](advice.md)。

🚨 **直す操作は、項目ごとに承認を取ってから、この skill の手順 7 の形でだけ行う**。`pkill` / `killall` / 広いパターンの kill を打たない。
tmux の `kill-server` / `kill-session` を打たない (本番のサーバ。`~/.claude/rules/tmux-probe-requires-socket-isolation.md`)。
アプリの再起動・設定の変更・sudo が要る操作は、人に頼む (未保存の作業を失うのは人なので)。

## 判定の語彙

`ok` / `注意` (今は壊れていないが放置すると困る) / `異常` (対処が要る) / `判定不能` (取れなかった。ok に丸めない) / `情報` (判定しない)。
`mac-healthcheck` の終了コード: 0 = 注意・異常なし / 1 = 注意か異常がある / 2 = 判定不能がある (1 より優先) / 3 = 実行環境の失敗。

## 手順

1. **置き場を決める**: `skill-tmpdir mac-healthcheck` が返す絶対パスを `<D>` とし、以降はリテラルで書く
2. **観点を選ばせる**: AskUserQuestion で次の 3 問を一度に聞く (どれも 1 問の選択肢は 4 つまで)
   - 「OS 全体のどこを診ますか」(`multiSelect: true`): ハードウェアと寿命 (`hw`。おすすめ: メンテナンス・買い替えの判断の材料) /
     プロセス・メモリ (`proc`。暴走・ゾンビ・メモリの増え方・カーネルのリーク) / ネットワーク (`net`。経路・DNS・LAN に開いたポート・Wi-Fi) /
     ディスク (`disk`。空き・スナップショット・掃除の候補)
   - 「よく使うソフトのどれを診ますか」(`multiSelect: true`): tmux (`tmux`) / nvim (`nvim`) / Chrome (`chrome`) / その他の常駐アプリ (`apps`。
     常駐量の上位と、動作中に更新で上書きされたアプリ)
   - 「重い検査も回しますか」: 回さない (推奨) / 回線の品質も測る (`-explicit`。networkQuality が約 20 秒・数百 MB 通信する。ネットワークを選んだときだけ効く)

   どれも選ばれない・回答が無いなら、何もせずに終える
3. **診断する**: 選ばれた観点をカンマでつないで (`<cats>`) 1 回だけ回す (ps を 5 秒あけて 2 回読むので、2 回回さない)
   `mac-healthcheck -categories <cats> [-explicit] -json -report <D>/checks.md > <D>/checks.json 2> <D>/checks.err; echo $? > <D>/checks.rc`
   (所要は 20〜40 秒。`-explicit` なら +30 秒)。rc=3 か `checks.json` が空なら、`checks.err` を見せて止める。
   選んだ観点に応じて、既存の道具も読み取りだけで回す (どれも何も消さない):
   - `disk`: `diskdoctor > <D>/diskdoctor.txt 2>&1` (掃除の候補を占有量の順に) と `tmpclean count > <D>/tmpclean-count.txt 2>&1`
   - `proc`: `svcdoctor > <D>/svcdoctor.txt 2>&1` (壊れた launchd の登録)。`proc.kalloc` が注意・異常なら `kernel-alloc-watch` で増え方の記録を見る
4. **寿命を判断する** (`hw` を選んだときだけ): `hw.model` の値の機種 ID (例 `Mac15,10`) で WebSearch を引き、発売年・Apple のサポートの区分
   (ビンテージ / オブソリート)・今の macOS とその次の版の対象かを確かめる (手元に情報源が無い。引けなければ「未確認」と書く)。
   それと `hw.battery`・SSD の摩耗 (`hw.ssd.*` の「摩耗」と「通電時間」)・`proc.pressure`・`proc.swap` から、[`advice.md`](advice.md) の基準で
   **このまま使える / メンテナンスを勧める / 買い替えを検討する** の 1 つに決め、根拠の項目を添える
5. **レポートを書く**: `<D>/report.md` に、上から次の順で書く
   - 総合: 手順 4 の結論 (hw を選んでいなければ省く) と、異常・注意・判定不能の件数
   - 対処の候補: 異常 → 注意の順に、項目・値・直し方 (JSON の `fix`)。tmux / nvim / Chrome の項目には [`advice.md`](advice.md) の助言を 1 行添える
   - 判定不能: 項目と取れなかった理由 (JSON の `note`)。直す手がある (smartctl が無い → `brew install smartmontools`) なら書く
   - 観点ごとの表: `<D>/checks.md` の本文をそのまま貼る。手順 3 の既存の道具の結果は要約を 1〜3 行
6. **報告して、直すかを聞く**: チャットに総合と対処の候補を短く出し、`<D>/report.md` の絶対パスを示す。続けて
   対処の候補 (異常・注意) が 0 件なら、聞かずに「直すものは無い」と伝えて終える (判定不能があれば、取る手を添える)。1 件以上なら
   **「おかしい箇所を直していきますか？」** と AskUserQuestion (`multiSelect: true`) で聞く。選択肢は対処の候補 1 件につき 1 つ
   (4 件を超えたら質問を分ける。1 回に 4 問まで。それでも余るものは重い順に残し、残りはレポートに残すと書く)。
   各選択肢の説明に「何をするか」「失うもの」と「誰がやるか (私が打つ / あなたの操作が要る)」を 1 行ずつ書く。
   質問の最後の 1 問として「人の作業が要るもの (アプリの再起動・バッテリー交換・買い替え・設定) を human issue にするか」を入れる
   (repo に `issues/` があるときだけ。無ければ聞かず、レポートに残す)。何も選ばれないなら、レポートの場所を伝えて終える
7. **選ばれたものを 1 件ずつ直す**。どれも、打つ直前に状態を取り直して、見せたものと同じかを確かめてから打つ:
   - プロセスを止める (親を失った nvim・言語サーバ・張り付いたプロセス): `ps -ww -o pid=,ppid=,etime=,comm=,args= -p <pid>` で同じ実行ファイル・
     起動からの時間が見せたとおりかを確かめ (pid の使い回しを除く)、引数を見て何者かを伝えてから `kill <pid>` (TERM)。消えなければ止めて報告する
     (自分から `kill -9` に上げない。上げるなら聞く)。引数に秘密が載りうるので、引数はチャットに出す前に読んで、トークンらしいものを伏せる
     🚨 nvim を止めるときは、未保存のバッファが無いかを先に人に確かめる (swapfile を無効にした設定では、止めた後に編集を回復できない。
     dotfiles の nvim は `swapfile = false`)
   - ファイルを片付ける (nvim の古い swap など): 消さずに `~/.Trash/mac-healthcheck-<日時>/` へ `mv` し、件数と移し先を伝える。
     動いている nvim が開いているファイルの swap は動かさない (`lsof <swap>` で開いているものが無いかを見る)
   - ディスクの掃除: cleanup-tmp-dir skill に渡す (一時領域・DerivedData・container)。`diskdoctor` の候補は、それが提示するコマンドを見せて人に打ってもらう
   - クラッシュが続く: crash-log-analyzer skill に最新の `.ips` を渡す
   - 人の操作が要るもの (Chrome・Screen Sharing などアプリの再起動、ファイアウォール、バッテリー交換、買い替え): 手順を 1〜3 行で示す。
     手順 6 で human issue が選ばれていれば、`issues/` の規約 (採番・`期限:`・具体的な手順) で起票する
8. **直した観点を測り直す**: 直した項目がある観点だけ `mac-healthcheck -categories <直した観点> -json > <D>/after.json 2> <D>/after.err` を回し (stderr を混ぜない。自動ビルドの知らせが stderr に出る)、
   前後の判定を並べて報告する。直らなかったものは理由と次の手を書く

## 読み方の注意

- **`proc.memgrowth` は 5 秒の 2 点**: ゆっくり漏れるものは見えない。疑わしいアプリは `-categories proc -interval 60s` で測り直してから漏れと言う
- **`hw.crash` はテストがわざと落とすものも数える**: dotfiles のテストは `bash`・`ps`・`sleep` を SIGSEGV などで落とす (`docs/macos-health-check.md` の 1 節)。
  名前だけで異常と言わず、`.ips` の `procPath` と親プロセスで仕分ける
- **`net.listen` の `*` は LAN から届く**: 開発用のサーバ (node の 3000 / 5173 など) は 127.0.0.1 で待ち受ければ届かない。`ARDAgent` (3283) は
  リモートマネジメント / 画面共有、`rapportd` は Apple 製品どうしの連係で、使っているなら意図どおり。
  🚨 見えるのは自分のユーザーのプロセスだけ: システムが開く待ち受け (画面共有 5900 など) は出ない。共有の設定はシステム設定 > 一般 > 共有 で見るよう添える
- **プロセスの名前は最長 16 文字** (ucomm。`Google Chrome He`)。引数や argv[0] には秘密が載りうるのでレポートに出さない。止める前に `args` を見るときも、チャットに出す前に伏せる
- **ゾンビ自身は kill できない**: 直す相手は `proc.zombie` に出る親。親がアプリなら再起動を人に頼む
