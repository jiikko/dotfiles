# 共通ルール

## 作業開始前の準備

- コードを書き始める前に、必ず `git pull` を実行して最新の状態に更新すること

## Git 禁止操作

- **無断で `git clone` しない。必要ならユーザーに許可を取ること**
- `git stash` を使用しない。ステージ済みの変更を退避したい場合は、別ブランチにコミットするか、ユーザーに確認すること
- サブモジュール内でコミットしたら、**必ずそのサブモジュールのリモートにも push する**。親リポジトリの push だけでは不十分。CI がサブモジュールの参照コミットを取得できず失敗する
- **コミット & push 前に `git status` で dirty なサブモジュールがないか確認すること**。dirty なサブモジュールがあれば、その中に入って差分を確認し、必要ならコミット & push してから親リポジトリの参照を更新すること。dirty を残したまま作業を終えない
- **commit / push 後は、成功を報告する前に実際の git state（`git log -1 --stat` / `git status` / push 出力）を確認すること**。ヘルパー関数やツール出力の「成功」表示を鵜呑みにしない（push 失敗や heredoc 破損を成功と誤報した実例がある）

## 並行作業者がいるときの worktree 退避

- **自分が作業を開始した後に、他の作業者（並行セッション・人間）によるファイル変更を確認できた場合**（例: 作業開始時点には無かった untracked ファイルが増えた、自分が触っていないファイルに新しい差分・ステージが現れた、自分の知らないコミットが積まれた）、**git worktree を作成してそこへ移動して作業してよい**（共有 working tree の index 競合・変更巻き込みを構造的に回避するため）
- **作業開始時点から存在する** dirty / untracked はこの条件に含めない（過去の作業の残骸かもしれず「今まさに並行作業中」の証拠ではない）。それらは従来どおり「触らない・巻き込まない」で共有 working tree のまま続行してよい
- worktree で作った**コミットを master ブランチへ移動できた時点で、作成した worktree は必ず削除する**（`git worktree remove`）。worktree を残したまま作業を終えない（放置 worktree は「どこに何があるか分からない」状態と stale ブランチを量産する）
- worktree を使わず共有 working tree に留まる場合は、pathspec 明示 commit の規律に従う（[`commit-with-pathspec.md`](rules/commit-with-pathspec.md)）
- **自分が書き込み権限のエージェント（`-s workspace-write` の codex 等）を 2 体以上並行させるときも worktree を分ける**。「担当ディレクトリが重ならない」を理由に同一 working tree で走らせない（想定外のファイルに手が伸びる / 並行中の build・test が何を検証したのか分からなくなる）。詳細は [`parallel-write-agents-need-worktree-isolation.md`](rules/parallel-write-agents-need-worktree-isolation.md)

## 応答・成果物の長さとスコープ

出力の長さ・スコープ・委譲の量は reasoning effort では制御できず（reasoning effort は思考量を制御するだけ）、明示指示でしか効かないので言語化しておく。寄る方向はモデルによって違う（Opus 5 は多く喋り・広げ・委譲する側、Fable 5.1 は逆に報告が少なくなる側）。出典: [Opus 5 ガイド](https://platform.claude.com/docs/ja/build-with-claude/prompt-engineering/prompting-claude-opus-5) / `claude-api` skill の Fable 5.1 節（乖離に気づいたら同じ commit で直す）。

- **応答は本題に紙面を使う**。前置き・免責・注意書きは短く。「説明して」に対しては要点のサマリを返し、詳細な解説は明示的に求められたときだけ書く
- **成果物ドキュメント（レポート・md・要約）の長さはタスクの必要量に合わせる**。中身は尽くすが、埋め草セクション・重複したサマリ・定型文で嵩上げしない
- **進捗更新のペース**: 最初のツール呼び出し前に何をするかを一文で言う。作業中は重要な発見か方針転換があったときだけ短く挟む。完了時は結論（何が起きたか / 何が分かったか）から書き、根拠はその後
- **依頼されたスコープをそのまま完遂する**。ルーチンな判断は自分で下し、解釈が分かれると成果物が実質的に変わるときだけ確認する。依頼が誤っている / もっと良い方法があると思ったら一文で述べてから依頼どおり進める。黙って狭める・広げる・別物にすり替えるのはしない
  - 例外は「コード変更時の自律改善」が実行を義務付けている範囲だけ（境界の正本はその節と [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md) の「自律改善との境界」表。ここで再掲しない）。範囲外の気づきは「ぼやきポイント」に回す
- **訂正のナレーションは重要なものだけ**。ユーザーのコード・結論・判断が変わる誤りは端的に訂正して続ける。何も変わらない言い間違いは黙って直して進む（過去の誤りの列挙・自己批判はしない）
- **自分の判断で自己再チェックの手順を足さない**。現行モデルは指示なしでも自分の作業を見直すため、その場の思いつきで「もう一度読み返すパス」「確認用サブエージェント」を積むのはトークンを増やすだけで品質を上げない。自発的にやる検証は具体的な外部事実の確認（実コマンド・テスト実行・実出力・git state）に寄せる
  - **これは既定のルールや明示起動した skill が定める検証・レビュー工程を省略する根拠にはならない**。「レビュー方針」の codex レビュー・敵対的レビュー、[`escalate-to-forge-after-failed-tries.md`](rules/escalate-to-forge-after-failed-tries.md) のエスカレーション、forge / cross-review / review-loop の各 Phase は要求どおり全て通す（これらはユーザーが選んだ工程であり、モデルの自己再チェックではない）

## 一時ファイルの配置

- **Claude がセッション中に作る成果物 (レポート・スクラッチ・中間生成物) は `./tmp`**。`/tmp` に置かない
  - **溜まったら `make clean-tmp`** (既定は 30 日より古いトップレベルのエントリ。`DRY_RUN=1` で一覧だけ、
    `DAYS=7` で期間を変える)。消す前に**結論が issue / コードへ移っているか**と、**issue や doc が指している
    パスでないか**を確かめる (`grep -rn 'tmp/' issues/ _claude/`)。放置すると溜まる:
    2026-09-02 に 309 エントリ / 831MB あり、うち 78 件 / 588MB が 1 か月超だった
  - 🚨 `tmp/` の ignore は **`~/.gitignore_global:5` 由来**で、repo の `.gitignore` には**無い**
    (実測)。新品チェックアウトと CI では ignore されないし、そもそも `tmp/` が存在しない
    (`src/glogx/worktree_status_real_test.go` の doc が CI 失敗 run 30823977760 を記録している)
  - **例外: ハーネスが指定する scratchpad** (`/private/tmp/claude-501/…`) はそのまま使ってよい。
    置き場所を選べない
- **スクリプト / テストが実行時に作る隔離ディレクトリはこの規約の対象外**。既定は OS の一時領域
  (`mktemp -d` / `t.TempDir()` / `os.MkdirTemp("")`)。`./tmp` に置くなら**理由をコード直近に残す**
  (実例: `src/glogx/worktree_status_real_test.go:repoTmpDir` — 使い捨て repo を repo 内に置く理由と、
  無ければ作る理由が書いてある)
- 線引きは置き場所でも `mktemp` かどうかでもなく、**終了時に消す責任が実装されているか**
  - 🚨 「`mktemp -d` は `/tmp` ではないから抵触しない」で判断しない。macOS は TMPDIR を外しても
    Darwin のユーザ専用一時領域 (`/var/folders/…`) を使い `/tmp` に来ないが、**Linux では
    `/tmp` 配下になりうる**。パスで線を引くと platform で答えが変わる
  - 🚨 消す責任は「`trap` を書いた」では終わらない。**`trap` は中断では走らず、dir を消しても
    そこで起こしたプロセスは残る** (`scripts/tmux_reap_orphan_servers.sh` の背景注記: `mktemp -d`
    の socket を消してもサーバが launchd に里子化して残り、自動復元が **17 日間**不発になった)

## Issue管理

- `issues/*.md` の内容に対応した後、作業が完了したら対応する issue ファイルを `issues/done/` ディレクトリに移動すること（ディレクトリ名が `issue/` 単数のプロジェクトでは読み替える）
- **issue の記述を鵜呑みにしない**。実際のコードと git 履歴に照らして検証してから着手する（既に修正済み・false positive を着手前に弾く）。関連: [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md)（refactor 提案の事前確認）/ [`issue-creation-codex-review.md`](rules/issue-creation-codex-review.md)（issue 作成時の codex レビュー）
- **人にやってほしい動作確認は応答本文に書いて流さず、issue に起こす**（chat は流れて存在自体が忘れられる）。`NNN-human-<スラッグ>.md`（人間しかできない作業のカテゴリ。動作確認・目視レビュー・外部サービスの操作・判断待ち）で起票し、本文に `期限: YYYY-MM-DD` を書く。**既読はファイルの位置で表す**（未読 = `issues/`、確認済み = `issues/done/`。既読ヘッダーは本文の書き換え忘れで嘘が残るので使わない）。期限切れはセッション開始時に hook（`_claude/hooks/human-tasks-due.sh`）が注入し、`issue-sync` skill でも最初に報告する。**hook が期限切れを出したらセッション冒頭で一言伝える**
- **実質的な作業をやり切ったら、セッションの振り返りを `NNN-retro-<スラッグ>-YYYY-MM-DD.md` に起票する**（chat の反省は流れて消える）。反省・気づき・改善案を書き、各項目に切り出し先（新規 issue / `_claude/rules/` / 却下）を提案するが、切り出しの実行はユーザーの判断を待つ。typo・数行の chore・調査だけのセッションは対象外。**done は「本文の残課題が空になったとき」**（実装の有無では判定しない）。未決着の retro はセッション開始時に hook（`_claude/hooks/retro-open.sh`）が注入するので、**古いものが溜まっていたらセッション冒頭で一言伝える**。書式の正本は `issues/README.md`
- 🚨 **retro の切り出し先は「既存ルールへの追記」を既定にする**。新規ルールを 1 本立てるのは
  **発動点 (トリガー) が既存のどれとも違うと言えるときだけ**で、言えないなら既存ルールの節として足す。
  理由は流入速度: `_claude/rules/` は実測で **2026-07-01 の 13 本 626 行 → 2026-09-03 に 37 本 2480 行**
  (65 日で 4 倍、直近 3 日で +445 行) まで増え、本文は毎セッション全文読まれる。増加の経路は
  ほぼ「retro の残課題 → rules へ切り出し」に集中している。**整理で削れるのは 1 回あたり百数十行**
  (2026-09-03 の監査の実測) で、**1 日ぶんの流入に負ける**ので、入口で絞る方が効く
  - 新規で立てるときは、retro の切り出し節に**なぜ既存へ追記できないか (発動点の違い)** を 1 行書く
  - 本文には規範だけを書き、実例・実測・起源は同名の `rules-rationale/` へ最初から書く
    (後から移す形は失敗している。実例: `rules-rationale/mutation-verify-new-tests.md` の
    「ルール本文から移した実例」節が文頭の切れた断片集になっている)
- **`issues/next/`（または group の `issues/epic/<name>/next/`）があるリポジトリでは、着手する issue をそこへ移して claim し、その移動だけを即 push する**（複数マシンが同じ issue 列を処理するため。claim は push されて初めて他マシンから見える。着手前に `git fetch` して既に next に居ないかを見る。group issue の claim 先は所属 group 内の `next/`、完了はどちらも global の `issues/done/`）。**どの `next/` も無いリポジトリ（仕事の repo 等）ではこの規律は適用しない**。詳細は [`claim-issue-in-next-and-push.md`](rules/claim-issue-in-next-and-push.md)。PostToolUse hook（`_claude/hooks/next-claim-push.sh`）が `issues/next/` への移動を検出して push を促し、UserPromptSubmit hook（`_claude/hooks/next-claim-unshared.sh`）が「他マシンから見えない claim」（未コミット / commit 済みだが未 push。glogx の `n` で人が付けたものを含む）を拾って push の可否をユーザーに伺わせる
- **設計判断・仕様・調査記録は `docs/`**（索引は [`docs/README.md`](../docs/README.md)）。触る前に読む制約
  (glogx の bubbletea v2 / テーマ色の定数 / tmux のセッション永続化) と、glogx の画面の契約がここにある。
  **新しく足したら索引に 1 行足す**（載っていない文書は存在を知っている人にしか届かない）
- **検証・監査・レビューのレポートを `./tmp` に出したら、結論・全数勘定・却下理由を issue （または対象コードのコメント）へ移すまでが 1 セット**。`tmp/` は gitignore なのでレポート本体は消える。特に「却下した指摘とその理由」は残さないと次の audit が同じ指摘を再生成する。詳細は [`move-report-conclusions-to-issues.md`](rules/move-report-conclusions-to-issues.md)

## 設計方針

- Godクラスを避けること。クラスが肥大化しそうな場合は、意味のある単位（責務ごと）でクラスを分割できないか検討すること
- 変更したファイルにGodクラス/Godファイルの予兆（責務の混在、過度な行数など）を見つけたら、リファクタリングを提案すること（ただし行数だけで判断せず、下記のとおり複雑性が実際に下がるかで判断する）
- **リファクタリングの目的は「複雑性を下げる」こと。行数が多いだけで単純にファイル/クラスを分けるのはリファクタではない**（分割は複雑性を移動するだけで削減しない）。「何をもって複雑性が下がるか」の判断基準と着手前の確認手順は [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md)
- バグフィックス後、そのプロジェクトに導入されているlinterのカスタムルールやpresetルールで再発防止できないか検討し、提案すること
- **zsh の hook (precmd/preexec) 経路から呼ぶ関数は `$(...)` でなく `REPLY` で返す**（fork が毎操作の体感レイテンシになる）。詳細は dotfiles repo の `rules/zsh-hook-return-via-reply.md`（dotfiles 固有の規範は `rules/README.md` が索引。zsh の trap 継承・Bench の見方もそこ）
- **カバレッジ向上を要求されても、対象が「テスト困難 かつ 低価値」の両方を満たすなら拒否する**（数値のための水増しテストを書かない）。判断は「テスト容易性 × 価値」の 2 軸で行い、困難×高価値は逃げずにテスタブルへ直してから書く。詳細は [`refuse-low-value-coverage.md`](rules/refuse-low-value-coverage.md)
- **検証は exit code ではなく「実行された証拠」で判定する**（exit 0 は「失敗しなかった」であり「そもそも走らなかった」を含む）。新設した検査は集約経路から実行して**その検査の出力が出ることを確認**する。`cmd | tail` の `$?` はパイプ終端の status。詳細は [`verify-execution-not-just-exit-code.md`](rules/verify-execution-not-just-exit-code.md)
- **新規テストは「壊す変更を 1 つ当てて red を見る」まで確認してから commit する**（green は「正しい」ではなく「その書き方では壊せなかった」）。変異させても green のままのテストは主張を何も守っていないので書き直す。詳細は [`mutation-verify-new-tests.md`](rules/mutation-verify-new-tests.md)
- **性能を主張するなら、実測値か「未実測である事実 + 実測の trigger」を残す**。数字なしで「削減した」と書かない（機能の issue で「動くはず」と書くのと同じ）。詳細は [`perf-claims-need-measurement.md`](rules/perf-claims-need-measurement.md)
- **計測・テスト用の shim / wrapper を PATH 先頭に置くときは、実体を絶対パスで解決してから exec する**（相対名だと PATH 先頭の自分自身に解決して無限再帰し、しかも無音で回り続ける）。解決結果が shim 自身の配下でないことを起動時に確認する。詳細は [`path-shim-must-resolve-real-binary.md`](rules/path-shim-must-resolve-real-binary.md)
- **外部コマンドの出力・終了コードを判定材料にするときは、stdout / stderr / exit code を最初から分離して測る**（`2>&1` や `| head` を通した観測を「実測事実」として設計に書かない。個別 CLI の仕様はルールでなく実装側のコメントを正本にする）。詳細は [`measure-external-cli-streams-separately.md`](rules/measure-external-cli-streams-separately.md)
- **再利用される道具（スクリプト / CLI / Makefile target / lint ルール / ヘルパー）を新設したら、同じ変更で「入口のドキュメント」を更新する**（その作業手順を持つ skill・領域の CLAUDE.md / README・既存ツールの一覧表）。**ツールのヘッダコメントは入口に数えない**（そのファイルを開く動機は存在を知っている人にしかない）。既存ツールと使い分けが要るなら判断基準を 1 行で書く。詳細は [`new-tool-requires-entrypoint-docs.md`](rules/new-tool-requires-entrypoint-docs.md)
- **端末 UI で全角と半角を同じ列に縦に並べない**（表示幅の合計が一致していても、半角 1 文字は全角 2 カラムのセルの左に寄るため目には揃わない。幅を数えるテストでは検出できず、人が見るまで分からない）。詳細は [`no-mixed-width-columns-in-terminal-ui.md`](rules/no-mixed-width-columns-in-terminal-ui.md)
- **テストを壁時計に依存させない**。経過時間で assert せず「何が起きたか / 何回 / どの順で」で判定し、何かを待つときは `sleep N` でなく**成立条件を上限つきでポーリング**する（`sleep` は速いマシンでは緑のまま通り、並列化や負荷で初めて落ちる）。詳細は [`avoid-wall-clock-assertions.md`](rules/avoid-wall-clock-assertions.md)
- **表示・レイアウトの意思決定は、本体へ入れる前にサンプルレンダラで回し切る**（本体に入れた時点でテストが「今の見た目」に張り付き、以降は見た目を変えるたびにテストを張り替えるコストを払う。凍結するのは見た目の判断だけで、ロジックはサンプルを待たずに書いてよい）。詳細は [`decide-layout-in-sample-renderer-first.md`](rules/decide-layout-in-sample-renderer-first.md)
- **対話プロンプトの確認は回答をパイプで流し込まない**（`printf 'y\n' | script -q` はパイプが閉じた時点で pty へ EOF を送り、`read` が空回答を受ける。「y を入れたのに中止された = 実装のバグ」に見える赤が出る）。pty driver で「プロンプトを待ってから書く」。詳細は [`verify-interactive-prompt-with-pty-driver.md`](rules/verify-interactive-prompt-with-pty-driver.md)


## コード変更時の自律改善

変更を入れる前に、変更先の周辺コードを読み、以下に該当するものがあれば **変更と同時に** 改善すること（提案ではなく実行）。

- **重複コード**: 同じロジックが複数箇所にある場合、まずヘルパー関数に抽出してから変更を入れる。同じ変更を2箇所にコピペするのは禁止
- **死んでいる分岐・未使用コード**: 変更対象の関数内に到達不能なコードがあれば削除する
- **不自然な責務の混在**: 変更を入れようとしている関数が明らかに複数の責務を持っていたら、まず分離してから変更を入れる

「依頼された変更だけ入れて終わり」ではなく、触ったコードを前より良い状態にして返すこと。ただし、依頼と無関係なファイルまで手を広げる必要はない。

## ぼやきポイント推奨

作業中に「依頼範囲外だが将来直したくなりそうな違和感」を見つけたら、応答の最後に **ぼやき（短い気づき）** として一言添えること。判断材料の提供であり、勝手に修正してはならない（依頼範囲外）。

- 対象例: 二重に実装されている規約・未統一のスタイル混在・ハードコード・マジックナンバー・テスト漏れの予兆・依存方向の歪み・命名の食い違い 等
- 形式: 「**なお、ぼやきポイント**: 〜」の一行〜数行。長文の分析にはしない。issue 化が妥当そうなら「issue 化しますか？」と一言添える
- 「タスクと無関係だから黙る」のではなく「無関係だが伝える価値があるなら一行ぼやく」
- 確信が低いもの・好みの問題・ユーザーが既知のものはぼやかなくてよい。ノイズになる
- **ぼやきも事実の主張なら裏を取る**。「〜は検査されていない」「〜が無い」のような不在の主張は、軽い口調ゆえに裏取りの敷居が下がる。取っていないなら「**未確認だが**」と明示する（誤ったぼやきは誤った判断材料であり、そのまま issue 化まで進むと反証コストを丸ごと払う。実例 2026-09-02: 「glogx の theme 色は機械検査の対象外」は誤りで、検査は `src/glogx/box_test.go` に在った — Go の検査は `tests/` ではなく `src/<proj>/*_test.go` にある）

## 不具合対応の原則

**パッチワーク（症状への対処）ではなく、構造的な根本改修を行うこと。** これは最も重要な原則の一つである。

不具合対応は「ログを足して現象を追う」より先に、設計上の前提（契約）を見直して構造で潰す。

- まず **不変条件（Invariant）** を言語化する（例：deep link は失われない／同一ファイルの同一性は一意／UI失敗で再生は止まらない）
- **失敗モード**（順序競合・再送・二重実行・部分失敗・再起動）を列挙し、設計で吸収する
- **境界（main/renderer、UI/Domain、外部API）** ごとに責務を分離し、手続きの連鎖ではなく「コマンド＋結果」の形にする
- 同一性は **安定キー（id/path_lower 等）** に統一し、表示用文字列に依存しない
- 追加ログは最後の手段。必要なら「イベント／状態遷移」が観測できる設計にする
- **「この if 文を足せば直る」と思ったら立ち止まる** — その条件分岐が必要になった設計上の前提を疑うこと
- **既存の呼び出しに新しい値・フラグ・経路を通す修正では、受け側のガード (preflight / reject) を先に grep で洗う** — 新しい呼ばれ方で初めて発火する既存ガードとの相互作用が悪化を作る。fake にも受け側の reject を模させる。詳細は [`survey-receiver-guards-before-passing-new-values.md`](rules/survey-receiver-guards-before-passing-new-values.md)
- 修正が「症状への対処」ではなく「前提の是正」になっているかを必ず確認する。場当たり的な条件分岐の追加や、特定ケースだけを救うワークアラウンドは原則禁止
- **直したバグは「同じ間違いが別の場所にもある」前提で grep する** — 同じ API・同じイディオムの使用箇所を横断確認する。特にテスト側で見つけた不具合は production 側を、production で見つけたらテスト・別モジュール側を必ず見る（実例: `SecItemDelete` の単発呼び出しを test helper で直した後、production の同一バグが残っていた）。**関数の契約変更（返し方・シグネチャ）も同じ扱いにする** — `echo` 返しを `REPLY` 返しへ変えたとき `$(...)` の呼び残しが 1 件出た（既存テストが検出。無ければ follow 抑止が静かに壊れていた）
- **効果がなかった修正は必ず revert する** — バグ修正を入れて検証した結果、効果がなかった（的外れだった）場合、その修正をコードに残さず元に戻すこと。効果のない変更が積み重なるとコードの意図が不明瞭になり、将来の改修を妨げる
- **UI / デバイス / 環境に関わる問題は、修正を提案する前に実際の環境制約（入力手段・ツールのバージョン・他 platform の参照実装）を確認する**。詳細は [`check-other-platform-reference.md`](rules/check-other-platform-reference.md) / [`no-osascript-for-ui-verification.md`](rules/no-osascript-for-ui-verification.md) / [`no-ios-simulator-verification.md`](rules/no-ios-simulator-verification.md)

## レビュー方針

- **重要なコード変更・バグ修正は、設計と実装の両方を外部レビューに通すことを基本とする**（設計 → レビュー → 実装 → テスト → レビュー）。レビュワーは、codex の使用がユーザーに許可されている環境では codex、それ以外では観点を分けた read-only サブエージェント（作法は [`issue-creation-codex-review.md`](rules/issue-creation-codex-review.md) の代替節）。指摘は無視せず、根拠の弱い断定・false positive を訂正してから commit する
- 起動は skill 経由（codex を使う環境では `codex-review` / `cross-review` / `review-loop` / `codex-lead` / `codex-drive`、下表参照）。codex を使わない環境では、観点を分けた read-only サブエージェントを直接起動する（`cross-review` skill は codex を含むため丸ごとは使えない）。typo・数行の chore など軽微な変更は対象外
- **レビューは「探す」だけで閉じない。壊しにいくパス（敵対的レビュー / red team）を 1 本混ぜる**。判断ロジック・境界・状態遷移・外部 I/O が動いた変更では、commit / PR クローズ前の最終ゲートとして通す（機械的置換・設定値変更だけなら省略してよいが、省略したことは一言明示する）
- **「指摘なし」は「正しい」ではなく「その探し方では壊せなかった」**として扱う。不変条件は「壊す方法が見つからなかった」ではなく、テスト・型・設計で固定して初めて閉じる
- **主張は証拠ではない**。自分/codex/エージェントの「対応済み」「検証済み」「テスト green」は、diff・実行結果・外部基準（spec の test vector / 実サーバ / CI）で裏を取ってから受け入れる。裏の取れない主張は「未検証」として報告に残す
- **自分で新設した「安全機構」(破壊的操作・後始末・検査/ゲート) は自己レビューで閉じない**。異常系 (対象 0 件 / 依存コマンド失敗 / 権限なし / 並行) を実験で作り、観点を分けた敵対的レビューを最終ゲートにする。詳細は [`adversarial-review-own-safeguards.md`](rules/adversarial-review-own-safeguards.md)
- **「冗長だから外す」と判断した防御は、外す前に「それがマスクしていた failure mode」を列挙する**。本来の目的に対して冗長なことと、他に何も守っていないことは別。残る防御が単一障害点なら、壊す変異が検出されるか確かめてから外す。詳細は [`list-masked-failure-modes-before-removing-guard.md`](rules/list-masked-failure-modes-before-removing-guard.md)
- **敵対レビューの出力こそ無検閲で採用しない**。発火条件（入力・順序・環境）が具体的で再現できたものだけ修正し、再現しないものは記録、示せないものは「未確認リスク」として issue / 観測ポイントに落とす。推測に基づく防御コードを足さない（作法の正本は `~/.claude/skills/codex-review/SKILL.md` の「敵対的レビューの作法」）

## スキルファイル参照

`~/.claude/skills/` に専門知識スキルが格納されている。以下のキーワードに関連するタスクでは、対応する SKILL.md を作業前に Read すること。

**エージェント (31 件) の一覧は [`agents/README.md`](agents/README.md)**。下の表は skill が主役で
agent は一部しか載っていないので、**agent を探すときはそちらを見る** (名前を知らないと呼べない
状態を避けるための入口。issue 001 の項目 21)。索引と実体の乖離は
`tests/claude/test_agents_index.sh` が検出する。

| キーワード | 参照先 |
|-----------|-------|
| 監査, audit, コードレビュー全体 | `~/.claude/skills/audit/SKILL.md` |
| コミット, commit, git commit | `~/.claude/skills/c/SKILL.md` |
| forge, 専門家実装, 専門家エージェントで実装/修正（修正・実装まで任せる） | `~/.claude/skills/forge/SKILL.md` |
| CSS, Node.js, Electron, フロントエンド, デスクトップアプリ | agent: `css-expert` / `nodejs-expert` / `electron-expert` |
| iOS, iPhone, XcodeGen, SPM, code signing, AVFoundation, @rpath | `~/.claude/skills/ios-app-developer/SKILL.md` |
| perf.log 分析, ボトルネック（ThumbnailThumb 専用 / bin/tt-client 前提） | `~/.claude/skills/perf-analysis/SKILL.md` |
| WCAG, アクセシビリティ, ダークモード, スタイルレビュー | `~/.claude/skills/style-review/SKILL.md` |
| AVFoundation, AVPlayer, 動画再生, seek, scrub, frame stepping | `~/.claude/skills/avfoundation-reference/SKILL.md` |
| watchOS, Apple Watch, WatchKit, WatchConnectivity, HealthKit, コンプリケーション | `~/.claude/skills/watchos-expert/SKILL.md` |
| App Store, TestFlight, 審査, リジェクト, App Store Connect | agent: `appstore-submission-expert` |
| issue-sync, issue同期, 完了漏れ, done移動 | `~/.claude/skills/issue-sync/SKILL.md` |
| fable, fableっぽく, fable流, Fable の働き方, /fable | `~/.claude/skills/fable/SKILL.md` |
| クラッシュ, crash, .ips, DiagnosticReports, SIGSEGV, SIGABRT | `~/.claude/skills/crash-log-analyzer/SKILL.md` |
| codex-review, Codexレビュー, コードレビュー依頼 | `~/.claude/skills/codex-review/SKILL.md` |
| codexにリード, codex主導で着手, 設計から codex に任せて（実装は Claude）, codex-lead | `~/.claude/skills/codex-lead/SKILL.md` |
| codexに書かせて, codexメインで実装, codexに作らせて, 設計から実装まで codex に丸投げ, codex-drive | `~/.claude/skills/codex-drive/SKILL.md` |
| cross-review, クロスレビュー, 複数視点レビュー | `~/.claude/skills/cross-review/SKILL.md` |
| レビューループ, review-loop, make review | `~/.claude/skills/review-loop/SKILL.md` |
| 視認性, 色被り, UXレビュー | `~/.claude/skills/ux-visibility-review/SKILL.md` |
