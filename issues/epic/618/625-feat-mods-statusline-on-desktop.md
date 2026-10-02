# 625 (feat): Claude desktop (Code タブ) にも、CLI と同じステータスバーを mod で出す

起票日: 2026-10-02

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

## 残り

- desktop の画面で見え方を人が確かめる: [628](628-human-verify-desktop-statusline.md) (期限 2026-10-09)。確かめられたら 625 も done にする

## 進捗

- 2026-10-02: 起票 (ユーザーの依頼)。mod の API で使えるものは型定義で確かめた: `$.process.run` は `stdin` を渡せる、`$.session.usage()` は status line と同じ値を返す、`$.ui.status` は plugin ごとに 1 行
- 2026-10-02: desktop の Code タブのこのセッションでは settings の `statusLine` が実行されなかった (80 秒間に起動された 5 件はすべて CLI 側)。描いていない見込みが高いので mod が要る側として進める (確定は人が画面で見る)
- 2026-10-02: mod を実装 (案 B)。テスト 10 本・変異 4 本。desktop の画面での確認を 628 (human) に起こした。claim は外した (人の確認待ち)
- 2026-10-02: 前提と受け入れ条件のチェックを実装・実測に合わせた (statusLine を実行しない / CLI で二重に出ない / テスト / 失敗時の見え方は済み。mod が desktop で読まれるか・描けるか・人の確認は 628 待ち)
