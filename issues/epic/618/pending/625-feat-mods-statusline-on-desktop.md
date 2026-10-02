# 625 (feat): Claude desktop (Code タブ) にも、CLI と同じステータスバーを mod で出す

起票日: 2026-10-02

> 保留 (2026-10-02、ユーザーの判断): mods 系の open issue をまとめて凍結した。再開するかどうかはユーザーが決める。

epic [618](618-design-claude-code-mods-migration.md) の子。619 の後。

## 概要

CLI では `_claude/settings.json` の `statusLine` (`~/.claude/statusline-command.sh`、587 行の bash) が、プロンプトの下にステータスバーを出している。
1 行目はディレクトリ / ブランチ / セッション名 / モデル / 文脈の使用量 / effort / advisor、2 行目以降は 5h・7 日の枠の消費ペース (色分け)。
Claude desktop の Code タブでも同じものを見たい。

## 前提 (未確認。最初に確かめる)

- [x] desktop の Code タブが settings の `statusLine` を描くかどうか。描くなら mod は要らず、この issue は設定の確認だけで閉じる
      → 実行されなかった (1 回の観測。下の「前提の実測」)。mod が要る側として進めた。画面での確定は [628](628-human-verify-desktop-statusline.md)
- [ ] desktop で mod が読み込まれるか。desktop が起こすセッションには `--plugin-dir` を渡せないので、`CLAUDE_CODE_PLUGIN_DIRS` (settings の `env` かプロセスの環境) で読ませる (reference.md「Developing one」)。619 の実測に desktop の経路を足す
      → 未確認。settings の `env` で読ませる形にした (619)。desktop のセッションで読まれるかは 628 で確かめる
      → desktop 同梱の claude (2.1.286) を単独で起こすと、desktop と同じ stream-json のモードでも settings の env だけでも読む (下の「628 の確認の結果」)。
        動いている desktop のセッションの中で読まれているかは未観測
- [ ] mod の表示の口が desktop で何を描けるか。`$.ui.status(text)` は plugin ごとに 1 本の行 (型定義では文字列 1 つ。改行と ANSI の色が desktop で通るかは未確認)。
      複数行・色が要るなら `AbovePrompt` の帯 (`ui.render`、`e.surface === 'desktop'` の要素の表) で描く
      → `AbovePrompt` の帯で描く形にした (3 行・色つき)。desktop で実際に描けるかは 628 で確かめる

## 対応方針 (前提の結果で決める)

**表示の中身は 1 箇所に寄せる。CLI の script と mod の 2 実装にしない** (`statusline-command.sh` には、ratelimit の色の閾値を
`src/ratelimit/usage/pace_drift_test.go` と揃える契約がある。写すと 3 つ目の実装になる)。

- 第一案: mod が `$.process.run({ argv: [statusline-command.sh], init: { stdin } })` で既存の script を呼び、出力を描く。
  stdin の JSON は CLI の `statusLine` が受けるものと同じ形を、`$.session.usage()` (文脈の使用量・枠・コスト。「status line が持つ値」と型定義に明記) と
  `$.session.model()` / `$.session.cwd()` 等から組む。CLI の JSON にあって `$` から取れない項目 (effort / advisor 等) は、取れる口を探し、無ければ desktop では出さない
- ANSI の色: desktop がそのまま描けないなら、script の出力の色を落とすか、mod 側で色付きの要素 (`Text` の色) に変換する。どちらにするかは見本を出して決める
  (`decide-layout-in-sample-renderer-first.md`)
- 更新のきっかけ: CLI は `refreshInterval: 60` と再描画のたび。mod は `turn.complete` と `$.clock.every` で呼び直し、結果を `$.state` に置いて描画は読むだけにする
  (描画の dispatch の中で外部の script を呼ばない)
- CLI では mod の表示を出さない (`e.surface` が `terminal` なら何もしない)。CLI の `statusLine` と二重に出さないため

## 受け入れ条件

- [ ] desktop の Code タブで、CLI と同じ項目が見えることを人が確かめた (desktop の描画は機械で確かめる手段が無い。確かめる手順を human の issue に起こす)
- [x] CLI の表示が変わっていないこと (二重に出ていない) — settings の `statusLine` は触っていない。mod は `terminal` では描かず `next(e)` を返す (テストで固定。CLI の画面を目では見ていない)
- [x] mod のテスト (`claude plugin test`) で、`desktop` の surface に描くことと `terminal` では描かないことを固定した (10 本、変異 4 本で red)
- [x] mod が読み込まれない・script が失敗したときに、desktop で何が見えるかを書いた (黙って消えるなら、その判断と理由を書く。618 の「黙って止まったときの扱い」)
      → script の失敗は帯に理由の 1 行を出す (下の「実装」)。読み込まれないときは何も出ない: 描く主体が無いので、mod の側では知らせようがない。
      気づく手段は、人が見て帯が無いこと (628 の「何も出ない」の手順) と、壊れた mod を validate / test で落とす `tests/claude/test_claude_mods.sh`

## 前提の実測 (2026-10-02 / desktop 2.19675.0、claude 2.1.286〜287)

- **desktop の Code タブのこのセッションでは、settings の `statusLine` が実行されなかった (観測。1 回)**: desktop の Code タブで動くセッション (`CLAUDE_PID=93948`、`CLAUDE_CODE_ENTRYPOINT=claude-desktop`) の作業中に、
  80 秒間 0.5 秒ごとに `ps -Ao pid,ppid,command` で `statusline-command.sh` を探した。見つかった 5 件の親はすべて CLI の claude (pid 88080 / 30763 / 63712) と、別の worktree のテストで、
  93948 の子も desktop の本体 (Claude.app) の子も無かった。`refreshInterval: 60` なので、描いているなら 80 秒の間に少なくとも 1 回は起動されるはず。
  → 描いていない見込みが高い。確定は人が desktop の画面で見て確かめる (下の受け入れ条件の human の issue と同じ手順でよい)
- **このマシンの制約 (619)**: user の mod に `ui.render` (AbovePrompt) と `$.ui.status` は届く (CLI の対話で実測)。desktop の surface で描けるか、`$.ui.status` の改行・色が通るかは未実測

## 実装 (2026-10-02)

- 見た目はユーザーが案 B (色つき) を選んだ (2026-10-02)
- `_claude/mods/desktop-statusline`: desktop のセッション (`$.session.surfaces()` に `desktop`) でだけ、`session.start`・メインの `turn.complete`・60 秒ごと (`$.clock.every`) に
  `_claude/statusline-command.sh` を直接起動する (CLI と同じく shebang の `env bash`)。stdin には CLI の `statusLine` と同じ形の JSON を、
  `$.session.cwd()` / `model()` (id を `Opus 5.5` の形に) / `usage()` (文脈の量・5h と 7 日の枠。リセット時刻は ISO からエポック秒へ) / `id()` と環境変数 `CLAUDE_EFFORT` から組む
- 出力の ANSI (文字色 30〜37・90 番台、背景 40 番台、太字・下線) を desktop の Text の `color` / `backgroundColor` / `bold` / `underline` に置き換えて、
  `ui.render` (AbovePrompt) で描く。色の閾値は script が決める (ここは写さない)。点滅は捨てる。terminal には描かない (CLI の statusLine と二重に出さない)
- script が失敗したら、帯に `ステータスバーを作れない: <理由>` の 1 行を出す (黙らない)
- テスト: `claude plugin test` 10 本 (ANSI の読み取り・JSON の組み立て・desktop でだけ呼ぶ・色つきで描く・terminal には描かない・失敗の理由・60 秒ごと)。
  変異 4 本で red (terminal にも描く / 色の置き換えを誤る / desktop 以外でも呼ぶ / 60 秒ごとを外す)
- CLI の JSON にあって `$` から取れない項目: `advisor` (script は settings から読むので影響なし)・`transcript_path` (空で渡す)
- 敵対的レビューは省略した: 表示だけの mod で、守りの機構でも、既存の挙動を変えるものでもない (CLI の statusLine と settings には触れていない)。失敗したときは帯が出ないか理由の 1 行が出るだけ

## 628 の確認の結果 (2026-10-02 23:25。何も出なかった)

desktop の Code タブのセッション (2026-10-02 23:19 に再開。`CLAUDE_CODE_ENTRYPOINT=claude-desktop`、Bash から `CLAUDE_CODE_PLUGIN_DIRS=~/dotfiles/_claude/mods` が見える) で、
入力欄の上には何も出なかった。ユーザーが画面で見たうえで、`screencapture -x` で撮った画面でも確かめた。`issue-band` の帯も出ていない
(ただし issue-band は `e.isInteractive` と `$.session.surfaces()` が空でないことで絞っているので、desktop で出ないことは切り分けの材料にならない)。

切り分けの観測 (どれも 2026-10-02。desktop 2.19675.0):

- **desktop が起こす claude は 2.1.286** (`~/Library/Application Support/Claude/claude-code/2.1.286/…/claude`)。CLI は 2.1.287
- **2.1.286 も mod を読む**。`env -i` から canary の印を見た: `-p` で読む / `CLAUDE_CODE_ENABLE_FUNCTION_HOOKS=1` を足しても同じ (2.1.286 の二進には rollout の switch で mods を止める文言があるが、このアカウントでは flag 無しで読んだ。2.1.287 にはその文言が無い) /
  desktop と同じ stream-json のモード (`--output-format stream-json --input-format stream-json`) でも読む / プロセスの環境に `CLAUDE_CODE_PLUGIN_DIRS` が無く settings の env だけでも読む。
  対照の 2.1.287 も同じ条件で読んだ。**版のせいで読まれない、ではない**
- 単独で起こした stream-json のモードでは、canary が見る `surface` は `null`、`isInteractive` は `false`。desktop は claude を `--await-initialize` で起こしていて、
  engine の二進 (2.1.286) を静的に読むと、desktop の surface の描画は desktop の client からの要求で動く (`ui.render … of <component> from desktop`、requestId つき)
- desktop アプリの `app.asar` にも mod の UI の部品の schema (`["Pane","AbovePrompt"]`) はある。desktop が描画を要求しているかは観測できていない
  (desktop のログ `~/Library/Logs/Claude/main.log` に mod の UI の行は 0。desktop のセッションの debug log は出ていない)

残っている候補 (どちらも未観測):

1. desktop が AbovePrompt の描画を要求していない (desktop 側の未対応・機能の出し分け。dotfiles からは直せない)
2. 要求は来ているが、mod が描く中身を一度も作っていない。`refresh` を呼ぶのは `$.session.surfaces()` に `desktop` があるときだけ (`onDesktop`) で、
   lines も error も null のままなら `ui.render` は `next(e)` を返す (何も描かない)

## 切り分けの結果と修正 (2026-10-03)

- **候補 1 は棄却**: dev-mods (`~/.claude/dev-mods/<session>/desktop-probe`。`e.surface === 'desktop'` の `AbovePrompt` に、中身を持たずに緑の 1 行を無条件で描くだけの mod) を、
  desktop の Code タブのこのセッションへホットリロードで載せたところ、**入力欄の上に出た** (ユーザーが目で確認)。desktop は `AbovePrompt` の描画を要求していて、`e.surface` は `desktop`
- **原因 (最有力。desktop 実機の値は未観測)**: 両 mod が、`session.start` の `refresh` と 60 秒ごとの timer を `$.session.surfaces()` (名簿) や `e.isInteractive` で「desktop / 対話か」を聞いてから起動していた。
  型定義によると、`session.start` の `surface` は `-p` / SDK では null、名簿に載る remote client は `session.attach` (最初の描画の要求) で後から加わる。desktop が起こすセッションは
  `-p` / SDK と同じ作りで、開始時には名簿が空・`isInteractive` が false のはずで、そうなら帯の中身は一度も作られず `ui.render` が `next(e)` を返す (何も描かない)。
  既存のテストは desktop のセッションを `surface: 'desktop'` / 名簿 `['desktop']` から始める fixture で書かれていて、この初期状態を一度も踏んでいなかった
- **修正** (commit「mods: desktop が起こすセッションを環境変数で判定する」): 「desktop が起こしたセッションか」を、起動時に決まる環境変数 `CLAUDE_CODE_ENTRYPOINT=claude-desktop` で聞く
  (desktop の Code タブのセッションで `claude-desktop` を実測。`$.env.get` で読める)。名簿・`isInteractive` には頼らない。描くかどうかは従来どおり描画のたびの `e.surface` で決める。
  `desktop-statusline` と `issue-band` の両方 (issue-band も session.start を `e.isInteractive` で、ターンの終わりを名簿で聞いていて同じ形)。
  script の出力が空 (rc=0) のときは、黙って何も描かず消えていたので、理由の 1 行を出す
- テスト: fixture を型定義どおりの desktop の開始 (surface null・isInteractive false・名簿空・環境変数 `claude-desktop`) に直し、CLI / `-p` では呼ばないことと、ターンの終わり・60 秒ごとが名簿なしで動くことを足した。
  変異 7 本が想定のテストで red (名簿に戻す変異は 6 本が red / 常に desktop 扱い / 空出力の guard 抜き / ターン側だけ名簿 / issue-band の 3 本)
- 敵対的レビュー (read-only の別視点 1 本。P1 / P2 なし、主要な修正は壊せなかった)。P3 は再現できていない推測なので防御は足さず、未確認リスクとして残す:
  (a) CLI で起こしたセッションに desktop の client が後から attach する形 (Remote Control など。推測) は、ターンの終わりの判定が環境変数だけになったので帯を作らない (旧: 名簿に desktop が載れば作った)。
  起きたら `turn.complete` の判定に `|| (await $.session.surfaces()).includes('desktop')` を足す /
  (b) mod の再読み込みと裏の `refresh` が重なったとき、`update()` が reject しうる (旧 `turn.complete` の `void refresh` から在る形。engine の挙動は未検証) /
  (c) issue-band の起動の裏の数え直し (約 1.5 秒) とターンの終わりの数え直しが逆順に終わると古い件数で上書きされる (稀。旧からある競合) /
  (d) desktop-statusline の起動時の `refresh` は、`$.session.model()` / `usage()` が headless の host でまだ答えられないと、次のターンか 60 秒後まで理由の 1 行が出うる (未検証)
- 追加: 返信やタイマーを待たずに作り直す `/statusline-refresh` を、desktop が起こしたセッションにだけ登録した (commit「mods: desktop-statusline に /statusline-refresh を足す」。結果 (成功 / 失敗の理由) を出力に返す。テスト 4 本、変異 4 本が想定のテストで red)。desktop 実機で `/` の候補に出て実行できることは未確認
- **未観測**: desktop 実機の `session.start` の `surface` / `isInteractive` / `$.session.surfaces()` の値 (2 本目の probe は `$.session.surfaces()` の中身を描かせたが、画面を読む手段が無く値を得ていない。
  `screencapture -x` は `could not create image from display` で失敗した)。原因は「最有力」であって、確定は新しい desktop のセッションで帯が出ること (628)

## 残り

- [628](628-human-verify-desktop-statusline.md) の手順で、**新しい** desktop のセッションで帯が出ることを人が確かめる (既存のセッションは旧版の mod を読み込んだままで、フォルダを見張らない。見張らせるには `CLAUDE_CODE_PLUGIN_DIR_WATCH=1` を env に足す必要があるが未実測)。
  出なければ 625 を開いたままにし、上の未観測の値を取るための probe を足す
- 確かめられたら 625 / 628 を done にする

## 進捗

- 2026-10-02: 起票 (ユーザーの依頼)。mod の API で使えるものは型定義で確かめた: `$.process.run` は `stdin` を渡せる、`$.session.usage()` は status line と同じ値を返す、`$.ui.status` は plugin ごとに 1 行
- 2026-10-02: desktop の Code タブのこのセッションでは settings の `statusLine` が実行されなかった (80 秒間に起動された 5 件はすべて CLI 側)。描いていない見込みが高いので mod が要る側として進める (確定は人が画面で見る)
- 2026-10-02: mod を実装 (案 B)。テスト 10 本・変異 4 本。desktop の画面での確認を 628 (human) に起こした。claim は外した (人の確認待ち)
- 2026-10-02: 前提と受け入れ条件のチェックを実装・実測に合わせた (statusLine を実行しない / CLI で二重に出ない / テスト / 失敗時の見え方は済み。mod が desktop で読まれるか・描けるか・人の確認は 628 待ち)
- 2026-10-02: 628 の確認で、desktop の画面には何も出なかった。版 (2.1.286) では読まれる、を A-B で確かめた。候補を 2 つに絞り、切り分けの観測を提案した (上の「628 の確認の結果」)
- 2026-10-03: dev-mods の probe で desktop が AbovePrompt を要求していることを確かめた (候補 1 棄却)。原因は desktop 判定を名簿 / isInteractive で聞いていたこと (最有力。実機の値は未観測) として、環境変数で判定する修正とテストを入れた (上の「切り分けの結果と修正」)。人の確認 (628) 待ち
