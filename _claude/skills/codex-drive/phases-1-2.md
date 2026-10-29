# codex-drive: 1. スコープ確定 / 2. 実装 / 2p. 競作

> `SKILL.md` (本体) から [1] に入る前に Read する。役割分担・大原則・モデル表・出力の置き場 (`<D>`) は本体にある。節番号 (`[3.8]` など) は本体のワークフローの図と同じ。

### 1. スコープ確定（Claude）

- **承認済み設計 (軽量パスなら確認済みアウトライン) を基に**、今回の **1 マイルストーン**を決める (例: 「probe が NEGOTIATE して交渉結果を出す」)。大機能は複数マイルストーンに割る。
- **受け入れ条件 = どう検証するか**を先に決める (プロジェクト標準 build/test green / CLI 出力 / CI E2E green / 実機ログ。
  Swift プロジェクトなら `swift build` / `swift test`)。
- **挙動保存リファクタで「移行前後の differential 比較」を受け入れ条件に持つなら、移行前の
  characterization (transcript / 期待値の固定) を最初の実装 commit より前に採取する**。
  移行 commit 後には「移行前」を動かす手段が消え、原理的に作れない
  (実測: obaket 651 — M2a 後に気づき characterization + 変異検証の代替で確定させた)。
- **(sandbox を外せない環境向け)** そのうち **codex 内で完結する検証を、codex が自分の sandbox で回せるか確認する**。
  既定の `-s danger-full-access` (下の節) では素のコマンドがそのまま回るので、この確認は要らない。
  **着手前に確かめるのは本流だけでよい**。`[2p]` の worktree と `[3.8]` の mutation worktree は
  cwd・cache path・依存取得先・fixture 位置が変わるので、**そのフェーズを実際に使うときに、
  そのフェーズの開始時点で**確認する (競作しないマイルストーンのために worktree を先に作らない)。
- **実行できた場合も、確定したコマンドを `[2]` のプロンプトに必ず貼る**。「失敗したときだけ書く」にすると
  codex が素の `swift test` に戻って再び未検証コードを返す。codex が「実行できない」と報告したら
  受け流さず、着手前の課題として扱う。
- 初回の確立は blind fix ではなく**環境診断**とする。実行 root・権限・cache の書き込み先・実際の exit code を観測してから
  コマンドを確定する (`instrument-before-second-fix.md`)。macOS/SwiftPM なら、例えば
  `CLANG_MODULE_CACHE_PATH="$PWD/.build/mcache" swift test --disable-sandbox --cache-path .build/spm-cache`。
  GUI・実機・実サーバ・CI 認証など codex 内で完結しない検証は Phase 0/[5] に委譲し、代替経路と未検証範囲を着手前に固定する。
  (出典: `573-retro-codex-drive-541-2026-08-24.md`)
- 🚨 **実装 run の sandbox は既定で外す: `-s danger-full-access`** (ユーザー決定 2026-09-13、dotfiles issue 369 案 0-b)。
  `-s workspace-write` では `make lint` / `make build` / `make test` (xcodebuild) が SwiftPM / clang cache の書込拒否で
  `Error 74` になり走らず (obaket 598, 2026-08-27)、**型エラー往復 2〜3 回 / マイルストーン (763)、走らない変異ループ
  (650 M1)、sandbox 由来の偽赤 (613)、「xcodebuild を試みさせない」の縛り (617 M3) がすべてここから派生していた**。
  外すと codex が自分でプロジェクト標準の build/test を回して反復できる。
  **プロンプト・事後検閲・git worktree は sandbox と同等の隔離を提供しない**。作業根外への
  書き込み、認証情報へのアクセス、他プロセスへの操作を事後検閲で取り消せるとは考えない。
  この既定は許可済みの信頼できるローカル repo での運用に限る。外部から持ち込まれたコード・
  未信頼の build/test を扱うときは workspace-write または外側の隔離環境を選び、ビルド上の不足を先に診断する。
  full access を選んだ場合も、次は編集事故の検出・軽減策として行う:
  - **worktree ごとに DerivedData を分ける**。codex が worktree で xcodebuild を回している間に Claude が本体で
    xcodebuild / `swift build` を回すと、DerivedData と package graph が共有されて**エラーではなく無限待ち**になる
    ([`no-concurrent-spm-build-during-xcodebuild.md`](../../rules/no-concurrent-spm-build-during-xcodebuild.md)、
    7.5 時間停止の実例)。`[2]` のプロンプトに「xcodebuild は `-derivedDataPath ./.derived` を付ける」を書く (雛形は `<<'EOF'` で
    クォートされているので `$ROOT` は展開されない。相対で書く)。`.derived/` は `.gitignore` か `.git/info/exclude` に
    入れる (入れないと untracked が `[3]` のスナップショット比較に混ざる)。
    Makefile が固定パスで付けられない repo では、**codex の run 中は Claude が本体で xcodebuild / swift build を回さない**
  - **作業根の外へ書かない**をプロンプトに明記する (`-C` で渡した worktree / repo root の外は読むだけ)。
    採用判定 `[3]` の前に **本体 checkout の `git status --short` が起動前スナップショットと同じ**こと、
    worktree の `git log -1` が起動前と同じ hash であることを確認する (sandbox が無いので、はみ出しは検閲でしか見えない)
    - **make target やラッパー経由の build を許すなら、中で走る道具の既定の出力先 (DerivedData・cache・`$HOME` 配下) まで確かめる**。
      作業根の外へ書く target は、作業根の中の出力先を指定できない限り使わせず、プロンプトで名指しして禁止する
      (「外に書かない」の一般則だけではラッパーの既定値を止められない。obaket 1016: `make -C macOS build` が既定の DerivedData に書いた)
  - **git 操作全般の禁止**は従来どおりプロンプトで (「git commit はしない」だけでは codex が `git pull --ff-only` を
    試みる。ThumbnailThumb 542)。sandbox があった頃は失敗していたが、いまは**成功する**ので検閲の項目に格上げ
  - 依頼していない破壊的操作 (rm -rf / 他 checkout の編集 / プロセス・tmux 操作) が要約と log に無いかを `[3]` で読む
    (`bin/tmux` shim は非 TTY からの本番 kill を拒否するが、それ以外は無防備)
  - 効いたかは計測で言う (issue 369 案 1)。obaket の次マイルストーンで Error 74 の有無・型エラー往復数・危険操作の有無を
    checkpoint に残す
- **sandbox を外せない環境 (danger-full-access を許可していないマシン / repo) では、旧規律で分担を最初に切る**:
  codex に任せるのは shared の `swift build` / `swift test` (上の cache フラグつき) まで、xcodebuild 系は **Claude が
  素の環境で回す**と `[2]` のプロンプトに書く。codex の全体 `swift test` が赤を報告しても sandbox 由来の偽赤が
  ありうる (613 で同日 2 回観測。素の環境では 0 failure) ので、赤も緑と同じく Claude の再実行で判定する。
  **codex に xcodebuild を試みさせない**とプロンプトに書く (試みると Error 74 の往復で時間を溶かす。617 M3)。
  codex に渡せる構文確認は **`swiftc -parse` まで** (型検査は依存モジュールが要り sandbox で組めない)。**型 error は
  Claude の `make test` で 1 往復として織り込む** (542: memberwise init の引数順 1 件で `passed 0` の往復)。
  shared SPM を触るマイルストーンでは、実装プロンプトに**「返す前に `cd shared && swift build --build-tests` を通す」を
  固定文で入れる** (sandbox で走るかは repo ごとに最初のマイルストーンで確かめてから固定する。issue 369 1-2)。
  この環境では未実行の理由が sandbox 由来なので、`[3.8]` の実行ループも最初から Claude のハーネスで当てる
  (変異 patch の生成だけ codex read-only に作らせる。codex に回しても typecheck だけで「成功」と書く: 650 M1)
- **sandbox の有無に関係なく `[2]` のプロンプトに書くこと / `[3]` で見ること**:
  **`project.pbxproj` は触らない (xcodegen が `project.yml` から生成する)** — 書かないと codex が新規テストファイルを
  pbxproj に手で登録する (obaket 686, 2026-09-01。再生成で上書きされるので実害は無いが diff がノイズになる)。
  codex が書いた macOS target のテストは **Claude が baseline 緑 + 変異 red を xcodebuild で確認するまで未検証扱い**
  (617 M5: codex 版の macOS テスト 2 本が baseline red / 変異検知が vacuous だった。macOS 側の変異は Claude が worktree で
  `xcodebuild -only-testing:<suite>` を当てる)。
  codex が `Package.swift` (依存の追加) を触ったら要約に明示させる (617 M5: test target の依存追加が要約に無く diff で気づいた)。
  **codex の要約に「未実行」「実行できなかった」がある成果物は、採用判定 (`[3]`) より先に Claude がその検証を回す**
  (645 M6c, 2026-09-03: 「macOS 側 xcodebuild は未実行」と明記された IA cancel test を、同ラウンドの shared 緑に
  引っ張られて後回しにし、hang の発見が 1 ラウンド遅れた)
- 触ってよい範囲・触らない範囲・既存方針 (設計 doc 等) を明示する。

### 2. codex に実装させる（write 権限）

```bash
# 最終応答・stdout・rc は **run 固有のパス** (<D>/<タスク>/。「出力の置き場」) に保存する。
# 後から「空振りだったのか遅れて完了したのか」を診断するため (2026-08-25 obaket 581 で証拠が残らなかった)。`</dev/null` 必須 (codex-review ルール)。
# 実装は書く run なので astra + effort **medium** (モデル表)。
# ROOT は repo root の絶対パス。cwd はサブディレクトリに残っていることがあるので `-C "$ROOT"` を必ず付ける
# (Storage/ を cwd に起動され macOS/bin を書けず「対応できませんでした」で 30 分ロス: obaket 696 項目 1)。
ROOT="$(git rev-parse --show-toplevel)"
last_message="<D>/<タスク>/impl.out.md"; log="<D>/<タスク>/impl.log"; mkdir -p "<D>/<タスク>"
command codex exec -s danger-full-access -C "$ROOT" -m gpt-6-astra -c model_reasoning_effort="medium" \
  --json -o "$last_message" </dev/null "$(cat <<'EOF'
<タスク>。git 操作 (commit / pull / checkout / stash 等) はしない (人間が検証して commit する)。作業根 (この repo root) の外には書かない。
ファイルを書き、プロジェクト標準の build/test が green になるまで自分で反復すること。Swift プロジェクトなら swift build / swift test を使う。
最後の確認はテスト全体で行う (関係しそうな suite だけを選んで代用しない。応答の形や request の列は contract / golden の stub が別の suite で作っている)。
xcodebuild を回すときは -derivedDataPath ./.derived (repo root 直下) を付ける。
issue は起票しない。範囲外の発見は要約に書く (採番から push までは人間側が 1 手で行う)。

## ゴール / 受け入れ条件
- <1 マイルストーンの完了条件と検証方法>

## 承認済み設計 (このマイルストーンに関係するスライス)
- <D>/design.<タスク名>.md の「<セクション名>」を読んでから着手すること
  (新規の実装 run は前フェーズの設計を知らない。継続 run も現行版を再確認する。貼らずにパス + セクション名で読ませる — トークン経済 2。
  軽量パス (設計フェーズ省略) のときはこのセクションごと省略可)

## やること
- <具体的な実装項目。spec があれば節番号で参照>

## 制約
- <触らない領域 / 既存方針 / 依存方針 / Linux ビルド維持 等>
- 構造方針: <[R] の判定を貼る。長期運用なら「症状パッチ・特例分岐で凌がず前提を直す。ただし今使わない
  抽象・設定可能化は足さない」/ 最小変更なら「既存構造に追従し、構造改修を持ち込まない」>
- production 型に **test 専用の状態・観測 API・barrier を足さない** (counter / waiter / 到達通知 /
  停止用 seam)。interleaving を固定したいときはテスト側だけで観測できる形 (bounded yield + flag、
  fake の注入、既存の internal 診断 counter) に留め、それでも固定できないなら「pin なし」として
  要約に書く。判断基準は `_claude/rules/refuse-low-value-coverage.md` の「テスト困難 × 価値」表。
- テストに実時間待ちを新規に書かない (`pollUntil(timeout:)` / `Task.sleep` / `.sleep(for:)` / 固定 deadline)。
  待つなら事象待ち (fake の waiter / `waitUntil*ForTesting`) + hang guard の `awaitOrFail`。実時間が検証対象そのもの
  なら integration test として分け、理由コメントを直前行に置く。
- 確証が持てない点は決め打ちせず ⓥ コメントを残し保守的に実装する。
- (挙動を変えない作業のとき) 旧実装の不具合を見つけても直さず、要約の別項目に書く。「挙動を変えない」とだけ書くと直す方へ寄る (obaket retro 1062)。
- 他の AI / CLI (claude / codex 等) を呼ばない。指示にない長時間の検証 (test-load 等) を回さない。
終わったら、変更点・検証結果・未完部分を要約。
EOF
)" > "$log.events.jsonl" 2>"$log"; rc=$?; echo "rc=$rc" | tee "$log.rc"; tail -40 "$log"
```

#### 同じ実装担当への継続

- 実装 run は `--ephemeral` を外して `--json` を付け、`thread.started.thread_id` をその run の checkpoint に保存する。
  独立レビュー・競作の各候補・テスト強化は引き続き別セッションにする。
- 同じ実装への修正は **明示 ID** で `codex exec resume <SESSION_ID> '<修正指示>' --json -o <新しい出力パス>` を使う。
  `--last` は並行セッションを誤って選ぶので使わない。resume も `</dev/null`、モデル・effort・一意なログ・timeout を付ける。
  resume の実行ディレクトリは元の worktree にし、記録した sandbox 設定を明示して再指定する。
  CLI 0.160.1 の resume は `-s` を持たないため `-c sandbox_mode=read-only` 等を使う。
  対象 CLI の `resume --help` で受け付ける設定方法を確認する。継続時の設定を暗黙の継承に任せない。
- 継続前に元 run の終了を確認し、`git diff` と現行要件・設計ファイルを読むよう指示する。
  rebase・他者の編集・設計変更があればその事実も渡す。session ID が失われた場合や前提が大きく変わった場合は、
  checkpoint + 現行差分から新規 run を起こす。停止した run の編集を全ロスと決めつけない。
- `--json` の command_execution / turn.completed / turn.failed / error と usage を保存する。
  完了イベント・応答があっても実装の正しさは [3] の build/test/diff で検証する。

- **モデルはマイルストーンの性質で振らない** (大原則のモデル振り分け): 機械的移植・boilerplate でも、
  判断を含む実装・非自明な wire/protocol でも、一律 `gpt-6-astra` + `medium` (モデル表の書く run)。
  - **唯一の例外は上振れ**: astra-mid の出力の質が低いと `[3]` の検閲で判定したマイルストーン
    (「動くが筋が通っていない」/ 捏造 API / 同じ型エラーを 2 往復しても直らない、等) は、
    **そのマイルストーンだけ `-m gpt-6.1-sol`** (effort は sol の `high`。モデル表) へ切り替えてよい
    (ユーザー指示 2026-09-13、dotfiles issue 369 1-3。2026-09-30 に上振れ先を astra から `gpt-6.1-sol` へ変更 (ユーザー指示: コストが安いため)。probe 実測: codex-cli 0.159.1 で rc 0、応答 "GPT-6")。
    切り替えは**マイルストーン単位**で、checkpoint に「M<n> は sol へ。理由: …」を 1 行残す (前後比較の材料)。
    下げる方向 (541 の low) は依然禁止。capacity 死はモデルを変えても起きうる (openai/codex #43398) ので回避策にはならない。
    駄目だった質の判定材料は「`[3]` で差し戻した回数と種類」であって印象ではない
  - 上振れ先は書く run の既定より 1 run の消費が重いことがある。長い実装の run を上振れで起こす前に codex の 5h 枠の残量を見る
    (`subagent-model-tiering.md` の「1 本の重さも見る」。obaket 1003 で astra の run が 2 回とも実装の途中で枠切れになった)
- `command codex` プレフィックス / `</dev/null` / `-o` の作法は **codex-review が正本**。独立レビューの `--ephemeral` は [2] の継続実装には付けない。
  `-o` は実行ログ全体ではなく最終応答 (`--output-last-message`) の保存先。標準出力/標準エラーは必要に応じて呼び出し側で保存する。
- **`--full-auto` は使わない**。実装は `-s danger-full-access` を明示 (ユーザー決定 2026-09-13、issue 369。理由と
  セットで守る項目は「1. スコープ確定」の sandbox 節)。外せない環境では `-s workspace-write`。review の `-s read-only` とは別。
- 大きいタスクは codex が時間内に終わらないことがある。プロンプトに「時間内に終わらなければ最小で動く形を優先し、
  残りは TODO で残す」と書く。
- **codex の exit 0 を「実装した」の証拠にしない (空振りの検出)**。実装フェーズの codex は、方針を述べただけで
  **exit 0・差分ゼロ・`-o` の出力ファイルも未作成**のまま終了することがある (実測 2026-08-24)。成功終了なので
  通知だけ見て次へ進むと「テストを足したつもり」で先へ進んでしまう。**期待した成果物 (`git status --short` の
  差分、または `-o` のファイル) が無い exit 0 は空振りとして扱い**、再投げか Claude 直筆に切り替える。
  🚨 判定は「差分ゼロ = 空振り」ではない (調査タスクや「変更不要」の結論では差分ゼロが正常)。
  **workspace-write の実装フェーズで、期待した成果物のパスに差分が無い**ときに限る。並行セッションの
  差分と区別する必要があるなら `[3.7]` の pre/post スナップショットと同じ形を使う。
  🚨 **空振りと判定する前に、その run が終了していることを確認する**。rc が確定する前に成果物の有無だけ見ると、
  **まだ走っている run を「空振り」と誤判定する**。fanout なら `runs.tsv` の rc、単発なら `$?` を保存してから成果物を見る。
  実害 (2026-08-25 obaket 581): 4 本を空振りと報告したが**少なくとも 2 本は遅れて完了**していた。
  さらに、空振り判定後に Claude が書き直したテストを、遅れて完了した codex の出力が同じパスに
  上書きし、**commit されたのは codex 版だった** (テスト名が同じで取り違えた)。誤判定のコストは
  待ち直しではなく「**codex が壊れている**」という誤った因果を作ること。
  🚨 **判定できるように、出力先は run 固有のパス (`<D>/<タスク>/`) にする**。`-o` と JSONL/stdout を scratchpad に出すと
  再起動で消え、「空振りだったのか」を後から検証できない (581 で実際に残らなかった)。一般則は
  [`verify-execution-not-just-exit-code.md`](../../rules/verify-execution-not-just-exit-code.md) の
  「非同期・background の完了も『成果物』で判定する」節。
- **異常終了 (capacity 死・timeout・中断) も「全ロス」と決めつけない**。書き込み権限の run (`danger-full-access` / `workspace-write`) は
  死ぬ前に**編集が途中まで、ときには全部残っている**。実例 (2026-08-25 obaket 571): 632k tokens 消費後に
  「Selected model is at capacity」で死んだ run は、突き合わせたら**要求項目が全部完了していた**。
  再投げの前に `git status --short` と要求項目を突き合わせる (全ロス前提の再投げは**二重編集**を作る)。
  起動し直すプロンプトには「再開の注意: まず git diff を読み、どこまで当たっているかを確かめてから続きを行う」を足す
  (obaket retro 1076: クレジット切れで途中で死んだ run を 3 回この形で起動し直し、3 回とも二重に当てずに続きから終わった)。
  上の空振り判定と同じ形で、違うのは向きだけ — あちらは「まだ走っている」を「壊れた」と読む誤り、
  こちらは「異常終了した」を「何もしなかった」と読む誤り。
  なお capacity で codex が使えない状態が続くなら、独立視点の agent で代替してよい
  (2026-08-25 の実績: 敵対 round と `[7]` 検収を agent で代替し、どちらも実害を検出した)。
- コマンド実行ツールの timeout は 900000ms。

### 2p. 実装の競作（N-of-M・方針が割れるマイルストーンのみ）

**同じマイルストーンを独立 2 本に実装させ、Claude が比較して勝者を採用する**。1 本しか走らせないと「その 1 案が
最良か」を比較対象なしに判断することになる (レビューは「書かれたコードの粗探し」しかできず、「別の書き方なら
そもそもこの問題が存在しない」には届かない)。競作は codex トークンを倍払って**比較対象**を買う手段。

**適用条件 (満たすなら既定で競作する。満たさないなら通常の `[2]` 1 本)**:

- 実装方針が割れる、**または割れる可能性がある** (データ構造・分割の粒度・同期/非同期・エラー伝播の設計に
  選択肢がありうる)。「確実に割れる」まで待たない — 割れるか不明なら競作して確かめる (2 本が同じものを
  書いたら「自由度が低かった」ことの証拠で、無駄撃ちではない。D1 の収束判定と同じ扱い)
- 受け入れ条件が**機械判定できる** (build/test/CLI 出力)。判定が主観なら勝者を選べないので競作の意味がない
- 機械的移植・rename・spec が実装を完全に決めるマイルストーンでは**やらない** (2 本が同じものを書くだけと事前に分かる)

**起点は `HEAD`**。開始時点の未コミット変更が今回の実装の前提になっているなら、**競作を使わない**
(または前提を先に commit してから競作する)。両候補は前提を見ずに実装するので、勝者を戻す段で初めて衝突する。

**必ず別の git worktree で走らせる**。同一 working tree に 2 本の書き込み run を当てると互いの編集を
上書きし合う。codex には `-C <worktree>` で作業根を渡す。
その repo に worktree を作るコマンドがあれば (例: my-products の `bin/wt create`。毎回新しい path + repo 直下の `.wt-setup` による
下準備)、detached で足りる用途 (実装 1 本・変異検証) ではそれを使う。下の例は branch を付ける競作用の手組み。

```bash
# --- 準備 (Bash 呼び出し 1)。パスは repo root からの絶対パスで確定し、出力された値を控える ---
root="$(git rev-parse --show-toplevel)"; stamp="$(date +%Y%m%d-%H%M%S).$$"
git worktree add -b "codex-drive/$stamp-a" "$root/../wt-$stamp-a" HEAD
git worktree add -b "codex-drive/$stamp-b" "$root/../wt-$stamp-b" HEAD
echo "A=$root/../wt-$stamp-a"; echo "B=$root/../wt-$stamp-b"   # ← この 2 行を控える
```

**`$stamp` は次の Bash 呼び出しに残らない**。控えた絶対パスを**リテラルで**書くこと (「並列起動の作法」)。
`../wt-*` を cwd 基準で書くと、cwd が repo のサブディレクトリのとき **repo 内部に worktree ができて**
本流の untracked を汚す。上記のように repo root 基準の絶対パスにする。

```bash
# --- 案a (Bash 呼び出し 2・run_in_background)。案b も同形で別呼び出し・別 worktree ---
wt="/<控えた A の絶対パス>"; out="<D>/<literal-stamp>.implA.md"; log="$out.log"
command codex exec -s danger-full-access -C "$wt" -m gpt-6-astra \
  -c model_reasoning_effort="medium" --ephemeral -o "$out" </dev/null \
  "<[2] と同じプロンプト>" > "$log" 2>&1; tail -40 "$log"
```

- **2 本には同じプロンプト (同じゴール・同じ受け入れ条件) を渡す**。案出し (`[D1]`) と違い、ここで軸を分けると
  「違う仕様のものが 2 つ」になり比較できない。振れ幅は codex 側の非決定性に任せる。
- **勝者の選び方 (Claude が判定する)**: ① 受け入れ条件を機械判定で満たすか (満たさない方は即脱落) →
  ② 承認済み設計のコントラクト (不変条件 / 失敗モード吸収 / 責務分担) に忠実か → ③ diff が小さく既存構造に
  馴染むか → ④ テストが循環していないか。**「codex のサマリがよく書けている方」で選ばない** (overclaim は既知の弱点)。
- **敗者剖検は codex にやらせる (検閲の前処理)**: 勝者と敗者の diff を渡し、「**敗者にだけある良い判断**
  (見落としていた異常系・簡潔な書き方・追加テスト・片方だけが踏んだ落とし穴) を列挙して、勝者へ移植する
  最小 patch 案を出せ」と read-only (sol・high) で 1 本回す。Claude は剖検結果を検閲して移植を判断する
  (2 本の diff 全文を並べて読む作業を digest 化する — 競作の主目的である「比較で見える落とし穴」を
  取りこぼさずに検閲単価を下げる)。移植の実施は `[2]` (codex) か trivial なら Claude 直接。
- **勝者を本流に持ち込む (patch 経由・codex には commit させない)**: 勝者 worktree には commit が無く、
  新規ファイルは untracked なので、`merge` / `cherry-pick` / 素の `git diff` はどれも取りこぼす。
  **`git add -A` で untracked を index に載せてから `--cached --binary` で patch 化し、本流に `apply` する**。
  適用前に必ず `--check` を通し、落ちたら**強制適用しない**。原因を `git apply --check -v` で切り分けて分岐する:
  - **適用位置のズレだけ** (本流が別の場所で進んだ) → `git apply -3` (3way) を試す。競合が残ったら手動で解消する
  - **本流の差分と同じ箇所が重なっている** → その本流側の差分が**自分の作業由来なら**手動で統合する。
    **見覚えのない差分 (並行セッションの可能性) なら触らず、状況をユーザーに報告して指示を仰ぐ** (commit-with-pathspec)
  - **どちらでも解けない** → 競作を中止し、勝者の設計メモだけを引き継いで通常の `[2]` (単発・本流の作業ツリー) で
    実装し直す。worktree は下記の手順で必ず片付ける

```bash
wt="/<控えた勝者の絶対パス>"; patch="<D>/<literal-stamp>.winner.patch"
git -C "$wt" add -A
git -C "$wt" diff --cached --binary > "$patch"
git apply --check "$patch" && git apply "$patch"   # --check が落ちたら適用しない
```

- **worktree とブランチは勝敗が決まった時点で必ず消す**。両 worktree は dirty なので通常の `remove` は拒否される。
  `git worktree remove --force` を使い、**`-b` で作ったブランチは別途 `git branch -D` する**
  (`worktree remove` はブランチを消さない)。放置しない (CLAUDE.md「並行作業者がいるときの worktree 退避」)。
  **削除は patch の適用と内容確認が済んでから**行う (`--force` は未移送の成果物ごと消す)。

```bash
git worktree remove --force "/<A の絶対パス>"; git worktree remove --force "/<B の絶対パス>"
git branch -D "codex-drive/<literal-stamp>-a" "codex-drive/<literal-stamp>-b"
git worktree list   # 消えたことを確認する
```

- 本流に載せたら `[3]` に入る。以降の検証・レビュー・commit は通常フローと同じで、commit は Claude が
  **自分が触ったパスを pathspec で明示して**行う (commit-with-pathspec)。競作したことは commit message に一言残す。
- 3 本以上に増やしてよいのは、方針が 3 つ以上に割れていて Claude が全部を精読できるときだけ。
  **律速は Claude の比較検閲**なので、読み切れない本数を走らせると質は下がる。

> 次の Phase ([3] 検証) に入る前に `phases-3-review.md` を Read する。
