# 762 (chore): codex-run / codex-fanout が、利用上限で落ちた run を「上限」と分かる形で返す

起票日: 2026-10-14

- 起源: obaket retro 1077 の提案 C

## 概要

obaket 1073 の作業で、`ratelimit -source codex -check` が rc=0 を返した直後に codex を 3 本起こしたところ、3 本とも約 30 秒で
`You've hit your usage limit` (再開 10/10) を出して落ちた。`ratelimit` が判定に使うのは 5 時間の枠で、この上限は見ていない。

落ちた run は driver の集計では他の失敗と区別が付かない。`bin/codex-fanout` は `runs.tsv` に rc を書き、失敗を `label:rc=N` で数えるだけで
(全本失敗なら「全 N 本が失敗」)、上限で落ちたことは各 run の log を読まないと分からない。`bin/codex-run` は fanout に委譲している。

## 対応方針

- 失敗した run の log に codex の利用上限の行 (`usage limit`) があれば、`runs.tsv` と stderr の要約に「上限による失敗」と出す。
  rc の値は変えない (呼び出し側の判定を変えないため)。文言は実測した log から採り、ここに転記しない
- 実装の前に決めること (反証レビュー 2026-10-14 の指摘。根拠の行は確認済み):
  - 出す場所: `runs.tsv` は label / rc / started_at / elapsed_s / out / log の固定 6 列 (`bin/codex-fanout` の冒頭の出力の説明)。
    列を足すと読む側とヘッダの説明が変わるので、列を足すか stderr の要約だけにするかを決める。`codex-run` は自分でも
    `codex-run: rc=%d ...` の行を出すので、そこにも出すか
  - 見る対象: `-J` の run は完了イベントの無い rc=0 を rc=65 にする。上限の行が `<label>.log` ではなく events に出る場合と、上限で落ちた run が rc=0 で返る場合の扱い
  - merger への注記: merger の prompt には失敗した run が `label:rc=N` で渡り「この観点は未実施」と書かせる。上限による失敗だと書き添えるか
  - rc: fanout は一部失敗で 2、全滅で 1 を返す。`codex-run` は 1 本なので 0 か 1。どちらも変えない
- `ratelimit` の判定 (`-check`) に weekly 系の枠は入れない。weekly を判断材料にしない方針 (`_claude/rules/subagent-model-tiering.md`) と衝突する
- テストは `tests/codex_fanout.bats` / `tests/codex_run.bats` に、上限の行を出して落ちる fake の codex を足す

## 受け入れ条件

- [x] 上限の行を含む log の run が、要約で上限による失敗と分かる
- [x] 上限以外の失敗 (rc=124 の時間切れ等) の表示は変わらない
- [x] 上の 2 つを fake の codex で固定し、検出を外す変異で red になることを確かめた

## 関連ファイル

- `bin/codex-fanout` (結果の集計: `runs.tsv` と `failed` 配列)
- `bin/codex-run`
- `tests/codex_fanout.bats` / `tests/codex_run.bats`

## 進捗

- 2026-10-15: 実装（commit「codex-fanout / codex-run: 利用上限で落ちた run を台帳と要約で上限と分かる形にする (issue 762)」）
  - 決めたこと: runs.tsv に 7 列目 `cause` を足す（`usage_limit` か `-`）。読み手は repo 内で `bin/codex-run` と bats だけだった（grep。`$6` を最終列として読む・NF を数える箇所は無い）。
    要約・merger の prompt の失敗表記は `label:rc=N(利用上限)`。上限で落ちた run があれば要約に本数と上限の行 (再開の時刻) を 1 行出す。rc は変えない。
    `codex-run` は runs.tsv の `cause` を読んで 1 行出す（検出は fanout の `usage_limit_line` の 1 か所）。merger の失敗も同じ判定にかける
  - 見る対象 (敵対的レビューを経た最終形。下の各周の結果): 時間切れ (rc=124) 以外で失敗した run の `<label>.log` の末尾 64 行。
    `-J` の run は codex-events が応答の本文を追記する直前の長さ (`<label>.stderr_bytes`。driver が inspect の前に控える) で切り、codex 自身が出した部分だけを見る。
    「tokens used / 数値」の 2 行を飛ばした後に連続する行頭 `ERROR: ` の行が**すべて** `hit your usage limit` を含むときだけ上限。
    `-J` の run は events のうち `type` が `error` / `turn.failed` の行も見る。上限で落ちた `-J` の run は error イベントで rc=65 になる（codex-events の判定）ので、rc=0 のまま見逃す形にはならない
  - runs.tsv は merger の行まで書いてから公開する (全滅 / merger 失敗 / digest 空 / 成功のどの終了経路でも)。codex-drive SKILL.md は「runs.tsv ができたら digest.md を読む」待ち方で、
    変更前は merger の実行中から runs.tsv が在ったので、待つ側が digest.md の無い時点で読みに行けた (3 周目の指摘。変更前からの食い違い)
  - 文言と終わり方の出典: slack-cli の `tmp/codex_review/err_004.log`（2026-10-07。末尾が ERROR の 2 行、アポストロフィは U+2019）と `tmp/adv-gate.20260916-131221.16791.stderr`（ERROR の 2 行の後に tokens used / 96,277、アポストロフィは ASCII）。
    実測した文言はこの 1 種類だけで、別の種別・プランの文言（"reached …" / 429 等）は未実測
  - テスト: `tests/codex_fanout.bats` 7 本 / `tests/codex_run.bats` 1 本 / `src/codexevents/driver_test.go` 4 本。bats の偽 codex は実 codex と同じく stderr の冒頭にプロンプトを出し、上限では ERROR の後に tokens used を出す
  - 変異 (最終形に対して。すべて戻して green): 時間切れの除外を外す → 時間切れの bats が red / 最後の塊でなく末尾の全行を見る → 引用と時間切れの bats 2 本が red /
    本文の印で止めない → Go の本文の引用のテストが red / tokens used を飛ばさない → 上限の bats 4 本が red / events を見ない → Go の events のテストが red。
    塊の全部でなく 1 行でも上限なら上限にする → 別原因の ERROR の bats が red / 末尾の空行を -J 以外でも飛ばす・区切り行で -J 以外でも切る → 同じ bats が red /
    runs.tsv を merger の前に公開する → 公開のタイミングの bats が red / `-J` の log を控えた長さで切らない → Go の本文引用のテストが red。初版の「検出を外す」変異でも issue 762 の bats 3 本と Go の events のテストが red になることを確かめた
  - 敵対的レビュー 1 周目（opus、read-only）の結果:
    - P1 採用: log 全体を grep すると、codex が stderr に出すプロンプト全文やコマンド出力に同じ文字列があるだけで上限と誤認する（このドライバ自身を codex にレビューさせて時間切れ → 「時間を置いて回し直す」と誤案内）。
      実ログで codex がプロンプトを `user` の見出しで出し、本物の行が末尾にあることを確かめた。初めは「末尾 40 行・行頭 ERROR・プロンプトにそのまま在る行を除く」にしたが、2 周目で崩れた (下)
    - P2-1 採用（同じ修正で解消）: 要約の行がプロンプトの古い引用から取られる
    - P2-2 記録: `-J` なしで上限に達した run が rc=0 で終わる形は未確認（rc の記録のある実ログが無い）。rc=0 の run は見ないので、その形なら上限と出ない。trigger: rc=0 で本文が空の run の log に上限の行を見たとき
    - P2-3 記録: 文言は 1 種類しか実測していない（上記）
    - P3-1 採用: events の grep に `-a`（NUL を含むファイルで Binary file の行が出る） / P3-2 採用: runs.tsv を一時ファイルに書いてから mv / P3-3 採用: 要約の「うち N 本」を「上限で失敗した run: N 本」に / P3-5 採用: codex-drive SKILL.md の exit code の節に 1 行
    - P3-4 却下: codex-run で上限の告知が fanout の stderr と 🚨 の行で 2 回出る。stderr を抑えると fanout 単体の利用者に届かず、codex-run の側は本文が空の案内と並べて読む位置にあるので両方残す
  - 敵対的レビュー 2 周目（opus、read-only）の結果:
    - P1 採用 (再現つき): 時間切れの run の log の末尾に、codex が実行したコマンドの出力として上限の行が残ると上限と読む → 時間切れ (rc=124) を候補から外し、「最後の塊」で判定するように変えた
    - P1 採用 (再現つき): `-J` では codex-events が応答の本文を log の末尾へ追記するので、本文が上限の行を引用すると上限と読む → `--- Codex JSONL result ---` より前だけを見る
    - P2 採用: runs.tsv の mv のコメントが「書きかけを見せない」と言っていたが、merger の行は後から追記される → コメントを「fan-out の行が揃う前を見せない」に直した (待ち方は変えていない)
    - P3 採用 (再現つき): プロンプトが今回と同じ上限の行を引用していると、内容での除外が本物まで消す → 内容での除外をやめた (位置で決める)
    - P3 記録: 実 codex がプロンプトを echo するときに行を変形する証拠は無かった (`prompt_issue_004.md` と err_004.log の echo 部分は一致)
  - 敵対的レビュー 3 周目（opus、read-only）の結果:
    - P2 採用 (再現つき): 非 `-J` の run でプロンプトに区切り行があると、そこで読むのをやめて本物の上限を見逃す → 区切り行で切るのは `-J` だけ、最後の区切り行で切る
    - P2 採用 (再現つき): codex の発言が上限の行の引用で終わり、直後に別原因の `ERROR:` で落ちると上限と読む → 塊が全部上限の行のときだけにした
    - P3 採用 (再現つき): 上限の行で終わるコマンド出力の後に外から kill される (rc=137) と、末尾の空行を飛ばして引用に届く → 空行を飛ばすのは `-J` だけ
    - P2 採用: runs.tsv の公開と SKILL.md の待ち方の食い違い (上の「runs.tsv は merger の行まで」)
    - P3 採用 (実測 154 MB で RSS 391 MB): log の全行を配列に持っていた → 末尾 64 行だけ
    - 未確認のリスクとして記録: 新しい版の codex が上限の ERROR 行の後に別の行を出すと見逃す (実ログ 2 本は v0.154.0)。trigger: 版を上げた後に上限で落ちた log を 1 本取って末尾の形を確かめる。
      `-J` の run で codex が stderr に上限の行を出すかは未実測 (テストの `CASE_LIMIT_STDERR` は作った値。events の判定が補う)
  - 敵対的レビュー 4 周目（opus、read-only）の結果:
    - P1 採用 (再現つき): `-J` で応答の本文が上限の行と区切り行の両方を引用すると、最後の区切り行が本文の中を指し、本当の原因 (stderr の別の ERROR) が上限で上書きされる。
      区切り行を最初で切ってもプロンプトの引用で外れるので、字面では決められない → 追記した driver 自身が inspect の前に log の長さを控え、そこで切る
    - P2 採用 (再現つき): `-J` だけ末尾の空行を飛ばしていたので、何も言わずに死んだ run も上限に数えた (非 `-J` と非対称) → 長さで切るようにして空行の特別扱いを無くした
    - P3 採用: ヘッダの「runs.tsv が在れば driver は終わっている」は言い過ぎ (終了コードはその後) → 「全 run と merger の rc は揃っている」に / publish_ledger の mv の失敗を黙らせず exit 1
    - P3 記録: 中断 (INT/TERM) が merger の実行中に来ると runs.tsv は作られない。意図どおりで、待つ側 (SKILL.md) は deadline と「プロセス不在かつ成果物無し = 死亡」で判定する
  - 敵対的レビュー 5 周目（opus、read-only。4 周目の修正の差分だけ）の結果: 長さで切る方式そのものは壊せず (マルチバイトの途中・古い値・merger の `-J` 経路・ラベルの衝突)。
    - 記録 (変更前からの性質): codex の子孫が codex の終了後にも stderr へ書くと、その行は控えた長さの後ろに入って判定から落ち、O_APPEND でない fd なので inspect の追記も上書きされる。
      実際の codex で子孫が残るかは未確認 (stub でだけ再現)。コードのコメントに書いた。trigger: 上限で落ちたのに cause が `-` の run を見たとき、log の末尾の形を確かめる
    - 採用: ヘッダの `stderr_bytes` の項が runs.tsv の説明の途中に入っていた → 移した / mv の失敗の exit 1 を終了コードの説明に足した
  - 敵対的レビューを 5 周で閉じた理由: 5 周目で判定の方式そのものへの指摘が尽き、残りは変更前からの性質 (上の記録) と文書のずれだけになった
