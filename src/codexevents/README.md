# codexevents

`codex exec --json` の出力 (events.jsonl) を読み、run が最後まで終わったか (complete) を判定して `meta.json` に記録する CLI。
`bin/codex-fanout` の `-J` (と `bin/codex-run -J`) が run ごとに呼ぶ。`-S` の schema が JSON のオブジェクトかの事前確認もする。

```sh
codex-events inspect <events.jsonl> <最後の応答のファイル> <meta.json> <log>   # 完了なら rc=0、未完了・読めないなら 1
codex-events is-object <file>                                                    # JSON のオブジェクトなら rc=0
```

スクリプトからは `bin/lib/codex_events.sh` の `codex_events_resolve` で起動時に 1 回だけ解決し、`"$CODEX_EVENTS"` で呼ぶ
(理由は `bin/lib/go_tool.sh` の冒頭)。

## 判定

完了 = `turn.completed` が最後のターンにあり、応答の本文があり、`turn.failed` / `error` のイベントも読めない行も無い。
本文は `-o` のファイルが空なら、最後の `agent_message` のイベントで補う (`response_source`)。

Python 版 (`bin/lib/codex-events.py`) を置き換えたもので、行の区切り (`str.splitlines`)・空白 (`str.strip`)・改行の統一
(`Path.read_text`)・ログに出すパスの表記 (`str(Path)`) を Python 版とそろえている (ログの行と `parse_errors` の行番号を変えないため)。
意図的に変えた点は issue 670 の「Python 版と変えたこと」。

## テスト

```sh
make test   # inspect の判定 (inspect_test.go) と、偽の codex で codex-run / codex-fanout を通す driver (driver_test.go)
make lint
```
