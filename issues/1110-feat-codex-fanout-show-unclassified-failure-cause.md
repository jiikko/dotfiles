# 1110 (feat): codex-fanout が、分類しない失敗の原因 (クレジット切れなど) を要約に出す

起票日: 2026-11-26

- 由来: obaket retro 1575 の提案 1 (2026-11-26 にユーザーが「片付けて」と指示し、retro の提案どおり起票)

## 概要

`bin/codex-fanout` の `runs.tsv` の `cause` 列は、分類できた原因だけを持つ。今の分類は `usage_limit` (codex の利用上限) の 1 つで、判定は `usage_limit_line` が
log の最後の `ERROR: ` の塊が全部「hit your usage limit」の行か (`-J` のときは events の `error` / `turn.failed`) で行う。それ以外の失敗は `-` になり、
読み手は rc と「全 N 本が失敗」しか見えず、個別のログを開くまで原因が分からない。

## 起きたこと (obaket retro 1575)

- obaket 1574 を codex-drive で進めた途中で、codex が `Your workspace is out of credits` で止まった。`runs.tsv` の `cause` は `-` で、rc=1 と「全 2 本が失敗」しか出なかった。
  原因は `<label>.meta.json` の `errors` にだけあった (`-J` の run。`src/codexevents/inspect.go` の `metadata.Errors` が events の error の行をそのまま持つ)
- `ratelimit -source codex -check` は直前まで rc=0 (5h 枠 0%) だった。クレジットは枠と別の会計なので、事前の検査でも見えない

## 対応方針

- 失敗した run の原因が分類できないとき、`cause` を `-` のままにせず、要約 (stderr の終わりの報告) に原因の候補の 1 行を出す。候補は `usage_limit_line` と同じ位置の規則で取る
  (log の最後の `ERROR: ` の塊の最後の行、`-J` なら `meta.json` の `errors` の最後の 1 件の message)。log の中のどこかにある語では決めない
  (プロンプトの echo・実行したコマンドの出力・応答の本文が同じ文言を引用しうる。`usage_limit_line` の冒頭のコメント)
- クレジット切れを `usage_limit` と同じく名前の付いた原因にする (`credits` など)。分類を足すときは、本物の失敗の log の形を測ってから照合を書く
  (今の照合は 2026-09-16 / 10-07 の実ログ 2 本から書いた。クレジット切れの log の形は、retro 1575 の run の出力が残っていなければ測り直す)
- `runs.tsv` の列の意味 (`cause` は原因が分かったときだけ値を持つ) を変えるなら、この台帳を読む側 (skill `codex-drive`・`forge-fanout` など) を grep してから変える。
  列を変えず、要約の行だけを足す方が受け手への影響が小さい
- テスト: 偽の codex でクレジット切れの行で落ちる run を作り、要約に原因の行が出ること / プロンプトの echo の中にだけ同じ文言がある run では出ないこと、の 2 本。
  変異で要約の行を消して red を見る

## 進捗

- [ ] 要約への原因の行
- [ ] クレジット切れの分類 (log の形を測ってから)
- [ ] テストと変異
