# zundamon-kaisetsu (Go)

台本 JSON を VOICEVOX で合成し、四国めたんとずんだもんの掛け合い動画 (HTML プレイヤー / mp4) を作る CLI。
**使い方・台本の書き方は skill 側** ([`_claude/skills/zundamon-kaisetsu/SKILL.md`](../../_claude/skills/zundamon-kaisetsu/SKILL.md)・
[`README.md`](../../_claude/skills/zundamon-kaisetsu/README.md))。ここは実装を触る人向けの地図。

## 起動の経路

`bin/zundamon-kaisetsu` (go_autobuild のラッパー) が、skill のディレクトリを環境変数 `ZUNDAMON_KAISETSU_SKILL_DIR` で渡して起動する。
テンプレート (`templates/player.html`)・立ち絵の既定の置き場・略語の一覧 (`acronyms.json`。`kana --check` が読む) はそこから読む
(読み替えの辞書 `readings.json` は Go が読まない。台本の `readings` へ合流させる手順は SKILL.md)。バイナリを直接起動するときも、この環境変数が要る。

```sh
zundamon-kaisetsu check | up | down | speakers | kana … | lint script.json | synth script.json | build script.json -o out --format html|mp4|both
```

## ファイルの地図

| ファイル | 持つもの |
|---|---|
| `main.go` | 引数の解析 (Python の argparse の再現)・シグナルと中断 (`appCtx`)・`Env` (テストで差し替える口) |
| `script.go` | 台本の読み込みと検証・キャラの設定・合成のキャッシュの鍵 (`Params` / `cacheSerialize`)・読み替え |
| `synth.go` | エンジンへの要求 (audio_query / synthesis)・キャッシュ (`<台本>.work/`)・`kana` |
| `lint.go` | `lint`: 台本の校正のうち文字列と構造だけで決まるもの (60 字超・「のだ」2 回・漢数字・図を指す言い方・独自調査のモードで資料の存在が分かる語 (目安) 等。issue 675 / 686)。試験用の台本は skill の `examples/review-bench/` |
| `source_check.go` | `lint --source 資料`: 台本と資料の突き合わせの候補 (資料に無い数字・英字の語、導入の区切りの言葉が無い。すべて目安。issue 685) |
| `reading_check.go` | `kana --script --check`: 英字の語を 1 文字ずつ読んだ行の警告と、英字の語の一覧 (略語の一覧は skill の `acronyms.json`。issue 673) |
| `engine.go` / `engine_auto.go` | エンジンのコンテナの起動・停止 / 自動起動と、使われなくなったら止める見張り |
| `build.go` | 組み立て (`assemble`: wav の連結・口の開き・プレイヤーのデータ)・`cmdBuild`・HTML への埋め込み |
| `mp4.go` | mp4 の書き出し (プレイヤーのまとめ撮りを Chrome で撮り、ffmpeg で切り分けてつなぐ) |
| `caption_speed.go` | 字幕が速くて読み切れないおそれのある行の警告 (build が出す。1 秒 7.5 字を超える行。止めない) |
| `blink.go` | まばたきの時刻表 (`blinkAt`) と、目を閉じているキャラのビットの列 (`blinkRuns`) |
| `show*.go` | 台本の `show` (図解: keyword / compare / list / code / image / mermaid) |
| `wav.go` / `outfile.go` / `chrome.go` | wav の入出力 / 出力ファイルの置き換え / Chrome の探索と PNG の大きさ |
| `pyjson.go` | Python 版と同じ値を作るための JSON・丸め・文字列化 (`py*`) |

プロセスの子孫を止める処理は共有 module の [`src/proctree`](../proctree/) (mermaid の描画の中断・時間切れ)。

## テスト

```sh
make test     # go test -race ./... (本物のエンジン・コンテナ・Chrome・ネットワークには触れない)
make lint
go test -run '^$' -bench . -benchmem   # 性能の傾向 (bench_test.go。合否には使わない)
```

- 外部コマンド (container / docker / npx / chrome / ffmpeg) は偽物を PATH の先頭に置く (`helpers_test.go` の `writeShim`)。
  mp4 の偽の Chrome / ffmpeg はテストのバイナリ自身を起こし直す (`mp4_test.go`)
- `testdata/*.json` は Python 版が出した golden (Python 版は消した。経緯は issue 641)
