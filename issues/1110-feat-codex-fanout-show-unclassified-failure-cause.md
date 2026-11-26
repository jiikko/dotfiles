# 1110 (feat): codex-fanout が、分類しない失敗の原因 (クレジット切れなど) を要約に出す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-26〜）

起票日: 2026-11-26

- 由来: obaket retro 1575 の提案 1 (2026-11-26 にユーザーが「片付けて」と指示し、retro の提案どおり起票)

## 概要

`bin/codex-fanout` の `runs.tsv` の `cause` 列は、分類できた原因だけを持つ。今の分類は `usage_limit` (codex の利用上限) の 1 つで、判定は `usage_limit_line` が行う:
追記前の stderr (`<label>.stderr_bytes` で切った log) の末尾 64 行から `tokens used` と数値の 2 行を飛ばし、最後の `ERROR: ` の連続が全部「hit your usage limit」の行か。
取れなければ `-J` の `.events.jsonl` を、行頭が `{"type":"error"` / `{"type":"turn.failed"` の行に絞って同じ語で探す。`meta.json` はこの判定で読まない。時間切れ (rc=124) は候補にしない。
それ以外の失敗は `-` になり、読み手は rc と「全 N 本が失敗」しか見えず、個別のログを開くまで原因が分からない。

## 起きたこと (obaket retro 1575)

- obaket 1574 を codex-drive で進めた途中で、codex が `Your workspace is out of credits` で止まった。`runs.tsv` の `cause` は `-` で、rc=1 と「全 2 本が失敗」しか出なかった。
  retro は原因を `<label>.meta.json` の `errors` で見つけた (`-J` の run。`errors` は `.events.jsonl` の `error` / `turn.failed` のイベントを丸ごと写したもの。`src/codexevents/inspect.go`)。
  当時の生ログは残っておらず、このメッセージのイベントの形と、stderr に出たかは未確認
- `ratelimit -source codex -check` は直前まで rc=0 (5h 枠 0%) だった。クレジットは枠と別の会計なので、事前の検査でも見えない
- 同じ症状の先行記録が obaket の retro 1210 にある

## 対応方針

- **`cause` 列には入れず、要約の行だけを足す**。今の表示は空でない `cause` を一律「(利用上限)」として stdout と merger のプロンプトに載せ、TSV の値はタブ・改行を escape しない。
  候補の文を `cause` に入れると誤表示になり、7 列の契約も壊れうる。既存の bats は未分類の `1/-` と merger のプロンプトの `bad:rc=1` を固定している
- **要約の行は、終了する全部の経路で出す**。全部の run が失敗したとき (`全 N 本が失敗。merger は回さない`) と merger が失敗したときは、末尾の要約より先に終わる。
  末尾にだけ足すと、起票のきっかけになった全滅の場面が直らない (今の `report_usage_limit` の呼び出しの位置に揃える)
- **出すのは「原因の候補」で、原因とは書かない**。log の末尾の `ERROR: ` の行は、実行したコマンドの出力を引用したものでもありうる (時間切れの run の fixture が既にある)。
  今と同じく rc=124 は候補にしない。log の中のどこかにある語では決めない (`usage_limit_line` の冒頭のコメント)
- `-J` の候補は、`errors` のイベントの形が一様でない前提で取る: `error` は `.message`、`turn.failed` は `.error.message` に文があり、文の無いイベントもある。
  最後のイベントが汎用の `failure` だけで、具体的な原因が stderr にある fixture もある (`CASE_QUOTED_MARK`)。JSON の破損・読めない場合は `parse_errors` / `read_error` に出る。
  どれを候補に出すかの順番 (stderr の `ERROR:` → イベントの文 → parse の失敗) を決めてから実装する
- クレジット切れを `credits` のような名前の付いた原因にするかは、本物の失敗の log の形を測ってから決める (今の照合は 2026-09-16 / 10-07 の実ログ 2 本から書いた)。
  足すなら、空でない `cause` を一律「利用上限」と表示する分岐も同じ変更で分ける。`runs.tsv` の第 7 列を機械で読むのは `bin/codex-run` (`$7 == "usage_limit"`) と
  skill `codex-drive` (rc と `usage_limit` で結果を確かめる)。`forge-fanout` は別の経路 (`_claude/workflows/forge.js` の `agent()`) で、この台帳を読まない
- テスト: ①全部の run がクレジット切れの行で落ちたとき、要約に候補の行が出る ②時間切れの run の末尾にコマンドの出力の `ERROR:` があっても出さない
  ③`-J` で、分類語を含む `error` の後に別の原因の `turn.failed` が続くとき、後者を候補に出す ④プロンプトの echo の中にだけ同じ文言がある run では出さない。
  変異で全滅の経路の候補の行を消して①が red になるのを見る

## 進捗

- [x] 要約への原因の行 (commit「feat(codex-fanout): 分類しない失敗の原因の候補を要約に出す」)
- [ ] クレジット切れの分類 (log の形を測ってから) — 未着手。本物のクレジット切れの log・events をまだ取れていない。取れたら `credits` の分類と、空でない `cause` を一律「利用上限」と出す分岐の分割を同じ変更で行う
- [x] テストと変異

## 結果 (2026-11-26)

- 候補の行: `codex-fanout: 分類しなかった失敗の原因の候補 (…):` の見出しの下に `  <label>: <行>` を 1 run 1 行。stderr に出し、全滅・merger の失敗・digest が空・末尾の要約の 4 経路すべてで出す (`report_failure_causes`)。
  `cause` 列・merger のプロンプトは変えない。時間切れ (rc=124) と上限に分類した run には出さない
- 候補の選び方: log の最後の ERROR の塊の最後の行 (上限の判定と同じ `last_error_block` を共用) → -J なら `codex-events error-text <meta.json>` の文を " / " でつないだもの
- `codex-events error-text` (新設): 文を持つ `error` (`.message`) / `turn.failed` (`.error.message`) の文を起きた順に 1 行ずつ (続けて同じ文は 1 行)。無ければ最初の `parse_errors`、無ければ `read_error`
- 上限の判定の events 側を「events.jsonl のどこかの error / turn.failed の行に上限の語」から「`error-text` の最後の行に上限の語」へ変えた (テスト③の要件)
- テスト: bats 3 本 (①全滅で候補 ②時間切れと ④プロンプトの echo だけの run で出さない、merger の失敗・digest 空・一部失敗の経路) + 上限の run に候補を出さない assert。Go: driver の `TestDriverJSONUnclassifiedFailureShowsCauseCandidate` (③ CASE_LIMIT_THEN_OTHER・CASE_LIMIT_THEN_GENERIC・CASE_QUOTED_MARK・CASE_FAIL) と `TestErrorTextListsEventMessagesInOrder`
- 変異 (mutate-verify で全部想定の assert が red): 全滅の経路の report を消す / failure_hint の rc=124 の除外を消す / events の上限の判定を旧実装 (どこかの行) に戻す / 候補の順を events 優先にする / error-text の走査を先頭からにする / 台帳の `elif` を `if` にする (上限の run に候補が重なる) / error-text が最後の 1 行だけを出す / digest 空の経路の report を消す
- 敵対的レビュー (opus) を 2 周。1 周目の P2 1 件・P3 7 件のうち、P2-1 (候補が汎用の文に隠れる)・P3-1 (meta.json を読めないことの握り潰し)・P3-2 (digest 空の経路)・P3-6 (入口の文書)・P3-7 (上限の run の候補のテスト) を直した

## 未確認のリスク (敵対的レビューで出て、直していないもの)

- **-J で error が上限の文、後の turn.failed が汎用の文 ("failure") だけを持つ形は、上限に分類しない** (候補には上限の文が残る)。旧実装は分類した。
  判定は「codex exec の JSONL が最後の error の文を turn.failed にそのまま写す」という仮説 (upstream の実装の記憶。未実測) に立つ。仮説の下ではこの形は本物で起きない。
  再提起の条件: 本物の -J の上限の run で turn.failed の文が error と違う events.jsonl。そのときは判定を「上限の文がどこかの行に在り、その後に別の具体的な原因が無い」に変える (`src/codexevents/errortext.go` の冒頭のコメントにも同じことを書いた)
- 次の形では候補が出ないか、引用を候補に出す (どれも本物で起きるかは未実測): log が空行で終わる / CRLF / `ERROR: ` で始まらない致命的なエラー (`Error:` `error:`) / `{"type":"error","error":"文字列"}` / 124 以外で echo の直後に死んだ run (基点から上限の判定も同じ形)
- 塊の最後が上限の行で、その前に別の原因の ERROR がある run は、上限の行を「分類しなかった失敗の候補」として出す (表示の見出しと中身がずれる)
- meta.json を読めない run では `codex-events: <path>: …` が 1 run につき 2 回 stderr に出る (上限の判定と候補で 2 回呼ぶ)
- 候補の 1 行は events の文の数に比例して伸びる (連続しない同じ文はまとめない。上限は付けていない)
