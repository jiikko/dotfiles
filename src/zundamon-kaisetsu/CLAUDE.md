# src/zundamon-kaisetsu を触るとき

地図とテストの回し方は [README.md](README.md)。ここには、知らないと事故る制約だけを書く。

## Python 版との互換 (issue 641)

- 🚨 **合成のキャッシュの鍵と、build が作るプレイヤーのデータは Python 版と同じ値にする**。`py*` (pyjson.go) は Python の
  `json.dumps` / `round()` / `str()` の振る舞いを再現するためのもので、Go らしい書き方に「直す」と鍵が変わり、利用者の
  `<台本>.work/` のキャッシュが全部作り直しになる。golden (`testdata/*.json`) との一致を落とす変更は、互換を捨てる判断として扱う
- 台本の行が `map[string]any` のまま渡るのも、Python 版と同じ順に値を引くための選択 (型付きの行に替えない。issue 654 で判断)
- まばたき (プレイヤーのデータの `blinks` と、キャラの `blink` / `blinkBit`) は Python 版の後で足したもので、golden の `frames` を
  変えないよう別の列に持つ。閉じ目の版の無い立ち絵ではどれも載せない (載せると golden の `cast` が食い違う)
- Python 版と意図的に変えた点 (文字列でない text を拒否する等) は issue 641 の「Python 版と意図的に変えたこと」

## Go と player.html の契約 (issue 650)

- `#sheet=` の書式 (`sheetFragment` / `mouthLevels`。欄は 行・話し中・口・まばたきのビット) と、図解の種類と読むキー (`showParsers` と `showAt`) は、Go と
  `_claude/skills/zundamon-kaisetsu/templates/player.html` の 2 か所にある。テストが突き合わせるのは字句まで:
  **字面を残したまま意味だけ変える書き換え** (まとめ撮りの分解で欄を並べ替える・`paint` の引数の順を変える) は検出しない
- プレイヤーの描画そのものに自動テストは無い (issue 645)。見た目を変えたら、HTML を作って Chrome で開いて確かめる

## 外部プロセス

- 🚨 **mermaid の描画を止めるときは子孫ごと止める** (`proctree`)。puppeteer は Chrome を別のプロセスグループ (detached) で起こすので、
  グループ宛ての kill では届かない (issue 649。実機で、グループ宛てだけでは Chrome が残った)。本体での `syscall.Kill` の直書きと、
  テストの `t.Parallel` (パッケージ変数を書き換えるテストがある) は forbidigo が止める (`.golangci.yml`。issue 661)
- 中断の判定: 全体の中断は `interruptedErr()` (appCtx)。**見張りは appCtx と別の ctx で止めるので、ctx を受け取る処理は
  `ctxInterrupted(ctx)` で見る** (appCtx で見ると、見張りの成功や失敗まで「中断」になる。issue 652)
- 自動で起動するエンジンは同時に 1 つだけ (印 `engine.auto` が URL を 1 つしか書けないため。コンテナの名前はポートごと)

## テスト

- 本物のエンジン・コンテナ・ネットワークに触れない (偽物は `helpers_test.go` の `writeShim` / `newFakeEngineEnv`)
- 実機の mermaid / Chrome を中断して確かめるときは、非対話の bash の `&` で起こしたプロセスは SIGINT を無視した状態で始まるので、SIGTERM を使う
