# 427 (feat): pro-con の本物の PM / PG の backend (段階 3〜5)

> 🚨 **担当中: dotfiles-5c**（2026-09-24〜）

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md) の「段階」3〜5

## 概要

模擬 (`src/pro-con/fake`) で UI とつなぎ込みを確かめた機能を、本物の Claude Code の session で動かす。
[425](done/425-research-claude-bg-remaining-measurements.md) の実測と [426](done/426-design-pro-con-open-decisions.md) の決定は済んだ (2026-09-24)。

## 範囲 (段階ごとに分けて入れる)

- [ ] 段階 3: 受付 PM 1 つ + カードとキュー + dispatcher (上限固定・手動起動)。質問への回答 UI と watchdog もここで入れる
- [ ] 段階 4: リソースの直列化 (PG が 2 体以上になると要る)
- [ ] 段階 5: 自動スケーリング (滞留と枠の残量で起動数を決める)

## 段階 3 の小分け (2026-09-24 夜に決めた。上から順に進め、済んだら [x] と commit を書く)

- [x] **3a カードの置き場所** (`src/pro-con/store`): daemon だけが書くカードの記録 (`$XDG_STATE_HOME/pro-con/live/cards.json`) と、受付の箱
  (`…/live/inbox/` に 1 件 1 ファイルの依頼)。PM / PG / 画面は箱に置くだけで、適用は daemon の 1 か所 (426 の決定 1)。
  採番と `card.Check` の不変条件の検査も適用の中。枠を使わず単体テストで確かめられる
  - 済み (2026-09-25): `src/pro-con/store` (Submit / Load / Apply)。依頼の種類は add / plan / ask / answer / review / close
    (分解済み → 作業中は daemon の仕事なので 3c)。遷移の規則は `transition` の 1 か所、不変条件は「新しく出た違反」で判定。
    テスト 6 本、変異 4 本が red (二重適用の控え / 不変条件 / 質問待ちでない回答 / 壊れた記録を空と読む)。
    敵対的レビューは 2026-09-25 に 3a〜3d をまとめて通した (下の「敵対的レビュー (3a〜3d)」節)
- [x] **3b `pro-con card` コマンド**: PM / PG が使う口 (`add` 依頼を積む / `ask` 質問を書く / `done` 終えた / `plan` 分けた 等)。箱に置くだけ。枠を使わない
  - 済み (2026-09-25): `src/pro-con/cardcmd.go`。置いた依頼の ID を stdout、使い方の誤りは箱に置く前に rc=2。
    テスト 3 本、変異 2 本が red (--issue の書式の検査 / add の必須の検査)
- [x] **3c `pro-con daemon`**: 箱の適用 → PG の起動 (`claude --bg -w`、431 の設定、`live.Register`) → `claude agents --json` で状態を読んでカードへ →
  回答・追記は stop → resume (426 の決定 2・3) → 落ちた回数で止める (決定 4) → watchdog。起動の部分は本物の claude が要る (枠を使う)
  - [x] 3c-1 分解済みのカードに PG を起動 (上限まで)・記録 (`live.Register`) に登録・回答を受けたカードは stop → resume。起動の口は差し替えられる形にし、偽物で単体テスト (枠を使わない)
    - 済み (2026-09-25): `src/pro-con/daemon` (Tick = 箱の適用 → 一覧に出た PG の登録 → 分解済みへの割り当て)。回答は card.Resume に入り、
      再開で渡したら空にする。起動の口は Launcher で差し替え、本物は ExecLauncher (`claude --bg -w … --setting-sources project,local`)。
      🚨 **ExecLauncher は本物の claude で一度も走らせていない** (3f で確かめる)。store に daemon 用の Update を足した (不変条件の検査は Apply と共通)。
      テスト 6 本、変異 3 本が red (上限 / 再開の分岐 / 起動の失敗で作業中にしない)。敵対的レビューは下の節
    - 登録の穴を塞いだ (2026-09-25): 記録を書き直すのは daemon 自身が起動・再開した直後 (pending) だけ。それ以外で pid が変わったら
      「外から操作された疑い」を知らせて書き直さない。最初の登録も、session の起動がカードの起動より前なら取り込まない。変異 2 本が red。
      🚨 pending は daemon のメモリの中だけ。daemon が起動し直すと失うので、その間に Claude Code が自動で再開した PG は「外から操作された疑い」に倒れる (3c-2 で扱う)
  - [x] 3c-2a 落ちた回数で止める (426 の決定 4)
      pid が変わった作業中の session の transcript に、Claude Code の自動の再開の文 (425 結果 1 の実測。`live.RestartNote`) が新しく出ていれば
      自動の再開として記録を書き直し、時刻を card.Crashes に数える。文が無ければ今までどおり「外から操作された疑い」。
      最後の起動・再開の後、30 分の間に 2 回落ちたら `claude stop` して、カードを人間の回答待ち (WaitCrashed) にする。回答すると同じ session を再開する。
      止められなければ作業中のまま次の Tick でまた試す。変異 7 本が red (再開の文を読まない / 前の文を数え直す / 時間の窓を外す /
      再開の前の回数も数える / 止められないのに回答待ちにする / 上限を 1 つ上げる / transcript の文を読まない)。
      🚨 再開の文を含むレコードの origin は未実測 (発言者を問わず探している)。Claude Code の版で文が変わると数えられず、外からの操作の疑いに倒れる
  - [x] 3c-2b watchdog (停滞)。進捗 = transcript の末尾に、それまでに無かった PG の出力が出たこと (`Transcript.LastNew`。同じ出力の繰り返しは数えない)。
      新しい出力が `card.StallThreshold(c, 15 分)` の間出なければ停滞にし、出たら外す。作業中の列を離れたら停滞の印を外す (store の遷移 / daemon の起動・再開)。
      `LastProgress` の書き手は daemon だけにした (live の画面側で transcript の最終更新を足していたのを外した)。
      変異 5 本が red (閾値を無視 / 戻っても外さない / 実行中の延長を外す / 列を離れても残す / 繰り返しも進捗に数える)。
      🚨 毎回少しずつ違う文を出すループは進んでいるように見える (415 論点 10 の「同じツール + 同じ引数」は未実装。ツール呼び出しは読んでいない)
  - [x] 3c-2c 知らせ (426 の決定 10):
      件数の文 (`daemon.Status`。例 `pro-con ?2 停滞1 🚨落ちた1`、何も無ければ空) を、変わったときだけ tmux のユーザー option `@pro-con-status` に書く
      (`_tmux.conf` は #() で fork しない方針なので、option を format で読む形。daemon が止まるときに外す)。
      新しく回答待ち (質問・権限・落ちて止めた) になったカードは macOS の通知を 1 度出す (待ちを抜けたら忘れる)。
      変異 6 本が red (変わらなくても書く / 同じ待ちを何度も通知 / 書けなかった文を毎回書き直す / 待ちを抜けても覚えたまま / 落ちた件数を数えない)。
      - [x] status の置き場所は、ユーザーが 3 案から「右端のキーガイドの島の左」を選んだ (2026-09-25)。`_tmux.conf` の status-right に、件数があるときだけ出す。
        キーガイドの島は 27 セルのまま。件数が最大 (2 桁ずつ) でも status-right-length 60 に収まることを tests/tmux/test_tmux.sh が展開後の幅で pin (変異 2 本が red)
      - [x] 3f の 2 回とも daemon の `tmux set-option` と `osascript` は失敗を返さなかった (daemon の出力に「書けない」「出せない」が 0 件)。🚨 画面に出たかの目視は未確認
      - daemon を kill -9 すると option が古い件数のまま残る (画面は daemon の最終 tick で古さを出すが、status には出ない)
  - [x] 3c-3 `pro-con daemon` の常駐と排他 (2 つ起動しない) — 3c-2 より先に済ませた (daemon を動く形にするため)
    - 済み (2026-09-25): `pro-con daemon [--limit N] [--once]` (`src/pro-con/daemoncmd.go`)。3 秒ごとに Tick し、何をしたかを時刻つきで stdout へ。
      排他は `daemon.lock` の flock (プロセスが終われば OS が外す)。変異 1 本 (flock を外す) が red。🚨 本物の claude では未実行 (3f)
- [x] **3d 画面**: 本物の backend が 3a の記録を読み、書き込み (回答・依頼 等) は箱に置く。読み取り専用をやめる
  - 済み (2026-09-25): 本物の backend のカードは store の記録から出る (session 1 本 = カード 1 枚はやめた)。作業中のカードに pro-con が起動した
    session の様子 (出力の末尾・pid) を足す。新しい依頼と回答は受付の箱へ、追加オーダー・btw・片付けはまだ (backend.Accepter で、押した時点で断る。
    ReadOnlier を置き換えた)。箱に適用待ちが溜まったらヘッダーで知らせる。隔離 tmux で card add → daemon --once → 画面の依頼の列、を確認。
    変異 3 本が red (外の session の様子を足す / 追加オーダーを受ける / 適用待ちを知らせない)
- [x] **3e PM への指示書**: PM の session に渡す、`pro-con card` の使い方と規律 (AskUserQuestion を使わない 等)
    - `src/pro-con/pm-guide.md` を `pro-con card guide` で出す (embed)。指示書に書いた `pro-con card ...` は TestPMGuideCommandsParse が今のパーサに通して、ずれを止める (変異: plan の例を `--issues` に崩すと red)。
      🚨 PM の session にこれを渡す手順 (起動時のプロンプトに入れるか) は 3f で本物の PM を立てるときに決める
- [x] **3f 本物の claude で通しの確認** (枠を使う。424 から引き継いだ受け入れ条件もここ)
  - 1 回目 (2026-09-25 08:34、Claude Code 2.1.281、PG 1 本): add → plan → daemon が起動 → PG が `card ask` → kill -9 → 回答 → 再開 → PG が `card review` まで通った (約 70 秒)。
    `claude agents --json` を 3 秒ごとに stdout / stderr を分けて記録した。実測で分かったこと:
    - 一覧の cwd は PG の worktree (`~/dotfiles/.claude/worktrees/pc-c-001`)。transcript はその project の下
    - **`claude --bg --resume <session-id>` は元の session を続けず、別の session id・別の短い id の session を立てる** (cwd は同じ、名前は AI の題に変わる)。
      stop した元の session は `--all` で stopped として残る。→ daemon が自分の再開の後の新しい session を拒んでいた (「短い id が別の session を指している」)
    - kill -9 の直後は `--all` なしの一覧にも pid 無し・`working` で出る (その間の cwd は repo root)。約 18 秒後に同じ session id が新しい pid で戻り、
      **startedAt は再開した時刻になる**。カードが質問待ちだったので daemon は記録を書き直さず、pid が古いままになった
    - `claude agents --json` が 3 秒を 1 回超えた (PG の起動・再開の最中)
    - PG は規律どおり `card ask` → turn を終える → 回答で再開 → `card review` を打った。除けた依頼は 0 件
  - 直した: 再開が返した短い id の session でカードの行を置き換える (live.ReplaceCard) / 再開の取り込みは同じ作業ディレクトリでも照らす /
    記録の書き直しは完了以外のカード全部で行う / 一覧の上限を 10 秒へ。変異 4 本が red
  - [x] 2 回目 (08:43、直した版): 同じ手順で通った。kill -9 の約 11 秒後に「C-001 の PG が落ちて自動で再開した (pid 72295 → 72978)」と数えて記録を書き直し
    (1 回なので止めない)、再開で新しくなった session (4e65ef8d) でカードの行を置き換えた。隔離した tmux (`-L pc3f`) で開いた本物のモードの画面に、
    C-001 がレビューの列に出た (daemon の最終 tick「0秒前」)。後片付け: 起動した 4 本の session を `claude rm`、worktree とブランチも消えたことを確認。
    状態の置き場は scratchpad へ退避し、`~/.local/state/pro-con/live` は空に戻した

## 敵対的レビュー (3a〜3d、2026-09-25)

1 周目 (Opus、読み取りのみ、4004f313〜13c8f60e) の全数: P1 1 / P2 5 / P3 4 = 10 件。対応 7 / 3c-2 へ 1 / 記録のみ 2。

- [x] P1 起動・再開が「失敗」と返っても session が立っていることがある / Launch の後で記録を書く前に落ちる → PG が Tick ごとに増える・回答が二重に渡る。
  **起動の前に印 (card.Launching / LaunchedAt) を記録へ書き、次の Tick で一覧と照らして取り込む** (起動は名前 `pc-<id>`、再開は前の session id)。
  一覧に出なければ launchGrace (1 分) 待ってから起動し直し、待つ間は上限に数える
  - 🚨 残り: 印を書いた後に立った session が launchGrace を過ぎてから一覧に出ると、起動し直した後なので取り込まれず孤児になる (回数の上限 3c-2 で本数は抑える)。
    daemon が Launch 成功の後に落ちて取り込むまでの間に PG が `card ask/review` を置くと、カードが作業中でないので除けられる (rejected/ に理由つきで残る)
- [x] P2 除けた依頼が控え (Applied) に入り、rejected/ へ移す前に落ちると理由を残さずに消える → 除けた依頼は控えに入れない。.reason を先に書き、rename の失敗はエラー
- [x] P2 pending がメモリだけなので、再開の後に daemon が起動し直すと二度と登録されない → 判定を記録の LaunchedAt に移した (pending は廃止)
- [x] P2 再開が、今の一覧と照らさずに短い id で `claude stop` を撃つ → prepare で、短い id が前の session id を指していなければ再開しない
- [x] P2 pending の間は別の session id でも取り込む → 記録の行と session id が違えば取り込まない
- [x] P2 (推測) `--resume` で短い id が変わると追跡が切れる → Resume が返した id をカードの Session にする。🚨 変わるかは未実測 (3f)
- [x] P3 1 回の Apply で控え (1000) を超えると二重適用 → 1 回 500 件まで
- [x] P3 起動に失敗し続けると History が増え続ける → 直前と同じ文は足さない (daemon.note。変異 1 本が red)
- 記録のみ P3 `daemon.lock` を稼働中に手で消すと 2 つ目が取れる (人の手の操作だけ。対応しない)
- 記録のみ P3 箱の依頼に送り手の認証が無い (どの PG でも別カードを操作できる)。規律で守る前提 (426)。3f で本物を動かして問題になったら再評価

変異 9 本が red (取り込まない / grace を無視 / 短い id の照合を外す / 一覧なしで割り当て / 外の pid 変更を書き直す / 自分の再開後も書き直さない /
起動より前の session を取り込む / 除けた依頼を控えに入れる / 1 回の上限を外す)。

2 周目 (Opus、直した差分 147fa851) の全数: P2 4 / P3 4 = 8 件。対応 5 / 3f へ 2 / 記録のみ 1。

- [x] P2 印の残ったカードが上限の判定の後ろにあると、立っている PG を数えずに別のカードを起動する → 印付きのカードを上限より先に片付ける
- [x] P2 除けた依頼を判定し直すと、後の依頼で状態が変わった後に適用される (順序の入れ替わり) → 除けた ID と理由を記録 (State.Rejected) に控え、
  判定し直さずに rejected/ へ移すだけにする。移せなくても daemon は止めない (結果に出して次の Apply で移し直す)
- [x] P2 前の session が一覧に無いのに `claude stop` を撃ち、失敗なら再開に届かない → 一覧に無ければ止めずに再開する
- [x] P3 テストの無い分岐 (adopt の「印より前」の除外 / 再開の失敗の後の取り込み) → テストを足した
- [x] 3f で実測: 自動の再開では startedAt が再開した時刻になる。`--resume` は別の session になる (上の 3f の節)。— 元の記述: 再開した session の `startedAt` が「再開した時刻」か「元の開始時刻」か (後者なら register が再開後の登録を拒み続ける。
  今のテストは前者を前提にしている)。`-w pc-<id>` を 2 回目に渡したとき (1 回目が worktree だけ作って落ちた) 起動できるか
- [x] P3 prepare のエラー (repo が設定に無い等) が Tick ごとに History へ 1 行足す → 上と同じ (直前と同じ文は足さない)
- 記録のみ P3 この修正より前に作られた作業中カード (LaunchedAt がゼロ値) は時刻の防御が効かない。本物の daemon はまだ一度も動いていないので、該当するカードは無い

変異 5 本が red (印付きを上限の後ろへ戻す / 一覧に無くても止める / adopt の時刻の除外を外す / 再開の取り込みを外す / 判定し直す)。
3 周目 (Opus、2026-09-25 08:05、afd41f39〜066c0393 = 2 周目の修正 + 3c-2a〜c) の全数: P1 0 / P2 3 / P3 4 = 7 件。全部対応した。

- [x] P2 前の session が一覧に無いとき stop せずに再開すると、Claude Code の自動の再開 (約 25 秒、その間一覧に出ない) と重なって同じ session が 2 本立つ →
  一覧に無い session の再開は、回答から restartWait (1 分) 待つ。🚨 自動の再開の途中が `claude agents --json` (--all なし) に出るかは未実測 (3f)
- [x] P2 再開の cwd を指定していない (daemon の cwd で走る。transcript が見つからない / 別の tree を書く) → 記録に session の cwd (Owned.Cwd) を持ち、
  そこで再開する。transcript が複数の project にあれば更新の新しい方を読む
- [x] P2 本物のモードでは card.Exec を誰も書かないので、長いコマンドの実行中を停滞と誤る。ツールだけの出力も進捗に数えていない →
  進捗にツール呼び出し (名前 + 引数) を足し、結果の返っていない呼び出しがある間は閾値を longToolLimit (1 時間) まで延ばす
- [x] P3 回数の追記と記録の書き直しの順 → 記録を書き直してから数える (あいだで落ちても 1 回数え漏れるだけ。逆の順だと pid を二度と書き直せない)。テストは無い (2 回の書き込みのあいだの停止を作れない)
- [x] P3 再開の文を発言者を問わず本文のどこでも探すので、依頼の原文の引用で数える → user レコードの文の先頭にあるときだけ数える
- [x] P3 止められないまま窓を過ぎると諦める / 止める前に短い id を照らさない → StopWanted の印で止められるまで試す。
  一覧で短い id が記録の session を指していなければ止めずに回答待ちにする
- [x] P3 tmux サーバを作り直すと件数が消えたまま → 変わらなくても 1 分ごとに書き直す

変異 10 本が red (再開を待たない / cwd を渡さない / transcript の先頭を取る / ツール呼び出しを数えない / 結果の返った呼び出しを実行中のまま /
実行中を延ばさない / 引用の文も数える / 窓を過ぎたら諦める / 短い id を照らさずに止める / 書き直さない)。
4 周目 (Opus、610ae2ab = 3 周目の修正) の全数: P1 0 / P2 3 / P3 4 = 7 件。対応 6 / 記録のみ 1。

- [x] P2 止める印 (StopWanted) が質問 → 回答 → 再開の経路で残り、再開した PG をすぐ止める → 作業中へ戻る settle で外す (store の遷移では外さない。戻る経路は settle 1 つ)
- [x] P2 一覧に無い = 止めた、と扱い、直後の自動の再開と重なる → 一覧に無ければ最後に落ちてから restartWait 待つ。過ぎたら「止めなかった (一覧に無い)」と分けて書く
- [x] P2 結果の無い古いツール呼び出しで PendingSince が立ったまま → 最後の PG の出力にある呼び出しだけを実行中に数える
- [x] P3 cwd が空のとき daemon の cwd で再開する → 再開しない (理由を履歴に書く)。🚨 agents の cwd が worktree を指すかは 3f で実測
- [x] P3 announce のコメントが挙動 (1 分ごとに言い直す) と食い違う → コメントを直した
- [x] P3 FindTranscript のテストが「末尾を取る」実装を見逃す → 新しい方を辞書順の先頭に置いた
- 記録のみ P3 同じ長いコマンドを繰り返すループでは、Tick が実行中を見るか結果の後を見るかで閾値 (1 時間 / 15 分) が揺れる。いずれ検出はされる

変異 5 本が red (settle で印を外さない / 一覧に無いのを待たない / cwd 無しで再開 / 置き去りの呼び出しを実行中に数える / 末尾を取る)。
5 周目 (Opus、0a18a2d1) の全数: P1 0 / P2 1 / P3 1 = 2 件。両方対応した。

- [x] P2 並列のツール呼び出しは 1 件ずつ時刻の違う別の行に書かれる (実 transcript で確認) ので、「最後の出力より前の呼び出しは置き去り」では長い方を落とす →
  置き去りの判定を「呼び出しの後に、ツールの結果ではない user 行 (再開の文・人間の発言) が来たか」に変えた
- [x] P3 記録の行に cwd が無いと二度と再開できない (前の版が書いた行 / 一覧の cwd が空だった) → 一覧に cwd が出ていれば行を書き直す

変異 3 本が red (結果で他の呼び出しも消す / 置き去りを消さない / cwd を埋め直さない)。
🚨 **周回はここで区切る** (adversarial-review-own-safeguards §7: 観測 API が絡むなら、周回を増やす前に実機を 1 回挟む)。残る不確かさは本物の claude の挙動
(一覧の cwd / 自動の再開の途中の見え方 / 再開後の startedAt / 短い id が変わるか) なので、3f で測ってから、その結果に合わせてもう 1 周攻める。

6 周目 (Opus、3f の実測を渡して 51084138 / 8f3f17e6 = 3f の修正を攻めた) の全数: P1 1 / P2 2 / P3 2 = 5 件。全部対応した。

- [x] **P1 再開の結果が分からない間に、人間が PG の worktree で開いた対話の session を取り込み、カードの行まで乗っ取る**
  (以後 `claude stop` や `--resume` の対象になる = pro-con が起動していない session に触る) → 取り込みと登録は `kind: background` だけ。
  cwd で照らすのは pro-con の worktree (`/.claude/worktrees/pc-`) の下だけ。自動の再開で記録を書き直すときは記録の cwd を優先する
  (落ちている間の一覧の cwd は repo root で、それを記録すると照合が repo 全体に当たる)
- [x] P2 cwd の照合の否定側のテストが無い → 対話 / 別の worktree / 印より前、の 3 ケース (一覧は候補だけ。元の session が先に当たると退行が隠れた)
- [x] P2 行を置き換える条件の守りが固定されていない → 短い id が使い回された形のテスト (`o.ID != c.Session`)。
  もう片方の `o.StartedAt.Before(c.LaunchedAt)` は、到達できる状態では前者と同時にしか外れないので、テストで区別できない (守りとして残す)
- [x] P3 履歴の重複除去が「claude を走らせて失敗と返った」まで潰す → 重複除去は「起動・再開できない」(何も走らせていない) の履歴だけ (noteOnce)
- [x] P3 コメントの「最大 3 秒」→ 10 秒

変異 7 本が red (対話も取り込む / worktree を照らさない / 印より前も取り込む / 対話も登録する / 使い回しで置き換える / 失敗の履歴も潰す / 一覧の cwd で上書き)。

7 周目 (Opus、6d7863ce。daemon 全体で「自分が起動していない session に届く経路」を攻めた) の全数: P1 0 / P2 1 / P3 2 = 3 件。全部対応した。

- [x] P2 起動の取り込みが名前 (`pc-c-001`) しか照らさず、別の状態の置き場で動く daemon が同じカード ID で立てた PG に当たる →
  cwd がこのカードの repo の worktree そのもの (`<repo>/.claude/worktrees/pc-<card>`) のときだけ取り込む。
  🚨 残り: 同じ repo を 2 つの状態の置き場で同時に使うと worktree そのものを共有するので、これでも区別できない (1 つの repo に daemon は 1 つ、が前提)
- [x] P3 stop / 再開の相手選びが pid を照らさず、登録の側 (pid が違えば外から操作された疑い) より甘い → 記録の pid と違う session は止めも再開もしない
- [x] P3 kind が background 以外の値 (版の変更・欠落) になると黙って取りこぼす → 短い id が一致するのに background でない session を見たら知らせる

変異 4 本が red (worktree を照らさない / 再開で pid を照らさない / 止めるときに pid を照らさない / kind の違いを知らせない)。

8 周目 (Opus、93b89fa8) の全数: P1 0 / P2 2 / P3 3 = 5 件。対応 3 / 記録のみ 2。

- [x] P2 止める段で、落ちている最中 (一覧に pid 0 で出る) の自分の PG を「記録と一致しない」と読み、止めずに回答待ちへ送る
  (直後の自動の再開で戻った PG が回答待ちの下で走り続ける) → pid 0 は一覧に無いのと同じく、最後に落ちてから restartWait 待つ。再開の側も同じく待つ
- [x] P2 起動の取り込みの cwd を文字列で比べていて、symlink を含む repo のパスで自分の PG を取りこぼす (→ 二重起動) → 両側の symlink を解決して比べる (samePath)
- [x] P3 repo の場所が分からないと worktreePath が空になり、空の cwd と一致して名前だけで取り込む → 空は何とも一致しない
- 記録のみ P3 落ちている間の一覧の cwd は repo root なので、最初の起動の直後に落ちて、かつ起動が失敗と返っていると、launchGrace と重なった時だけ取り込めず二重に起動しうる。
  取り込まない側に倒れるので外の session には触らない。回数の上限 (3c-2a) で本数は抑える
- 記録のみ P3 kind の警告は Tick ごとに daemon のログへ出続ける (pid の警告と同じ扱い。履歴・通知には入らない)

変異 5 本が red (pid 0 の間も止めに行く / pid 0 の間も再開する / symlink を解決しない / 空も一致と読む / repo の場所なしで取り込む)。

## 前提

- **設計の決定は 426 の「決定」節が正本** (カードの書き手は daemon / 回答は stop → resume / 追記は turn の区切り / 落ちたら自動の再開 + 回数で止める /
  役割 4 つ (PG・テストの係・調べる係・レビューの係) / PM 1 つ / 知らせは tmux の status + macOS の通知)。ここには写さない

- **PG / 係を起動したら、`live.Register` で pro-con の記録 (`$XDG_STATE_HOME/pro-con/live/sessions.json`) に足す**。本物のモードは記録にある session
  だけを扱う ([424](done/424-feat-pro-con-readonly-real-backend.md) の範囲の変更)。記録の書き手は daemon だけ (426 の決定 1)
- **記録には session id と pid が必須** (`live.Register` が拒む)。`claude --bg` が返す短い id で `claude agents --json` を引き、session id と pid を得てから記録する。
  再開のときも CardID を渡す (書き直しは行ごと置き換える)
- **pid は落ちて自動で再開されると変わる** (425)。変わると pro-con の session は一覧から外れる (失敗側)。自動の再開 (transcript に
  「automatically restarted」の注記が入る) なら新しい pid で記録を書き直し、外での再開なら書き直さない、を区別する
- **外からの操作を検出する**: pro-con の session は、他の shell からも `claude attach` / `claude stop` できてしまう (Claude Code の側で止められない)。
  pro-con が指示していない変化 (止まった・入力が増えた・transcript に pro-con の知らない人間の発言) を見つけたら、カードに「外から操作された」と出して止める
- PG / 係は [431](431-feat-pro-con-pg-session-settings.md) の役割ごとの設定で起動する (431 を先にやる)
- 模擬 (ハリボテ) は残して起動のときに選ぶ。起動の口・状態ファイルの分け方・画面の区別は [424](done/424-feat-pro-con-readonly-real-backend.md) の「模擬と本物の併用」節

### 415 の決定事項

- PG は `claude --bg -w` で起動し、自分のブランチまで push する (master へは PM がレビューしてから)
- 同時実行数の上限は 2 から始める
- dispatcher と watchdog は決定論的に作る (LLM に見張らせない)

## 実測で分かった前提 (425)

- PG の状態は `claude agents --json --all` の `status` / `state` / `pid` / `waitingFor` で読める (完了・API エラー・停止・プロセスの死・権限待ち・質問待ちを区別できる)
- PG への送信は SendMessage (bg session も `ListAgents` に名前で出る)。枠は `claude -p "/usage"`
- PG にもユーザーの hook と規約が効く (起動時 約 13 万 token。Stop hook が別の作業の issue を更新させにいく)。PG 用の `--settings` で hook を絞るかを決める

## 受け入れ条件 (424 から引き継ぎ)

- [x] pro-con が起動し、記録 (`sessions.json`) に書いた本物の bg session が、本物のモードのカードに出る (3f の 2 回目) (424 では記録を書く側が無く、実物で確かめていない)

## 関連ファイル

- `src/pro-con/backend/backend.go` (UI との口。模擬と同じ interface を満たす)
- `src/pro-con/fake/` (振る舞いの手本)

## 進捗

- [x] 着手できる (425 の実測・426 の決定が済んだ。2026-09-24)
