# zundamon-kaisetsu (Go)

台本 JSON を VOICEVOX で合成し、四国めたんとずんだもんの掛け合い動画 (HTML プレイヤー / mp4) を作る CLI。
**使い方・台本の書き方は skill 側** ([`_claude/skills/zundamon-kaisetsu/SKILL.md`](../../_claude/skills/zundamon-kaisetsu/SKILL.md)・
[`README.md`](../../_claude/skills/zundamon-kaisetsu/README.md))。ここは実装を触る人向けの地図。

## 起動の経路

`bin/zundamon-kaisetsu` (go_autobuild のラッパー) が、skill のディレクトリを環境変数 `ZUNDAMON_KAISETSU_SKILL_DIR` で渡して起動する。
テンプレート (`templates/player.html`)・立ち絵の既定の置き場・読み替えの辞書はそこから読む。バイナリを直接起動するときも、この環境変数が要る。

```sh
zundamon-kaisetsu check | up | down | speakers | kana … | synth script.json | build script.json -o out --format html|mp4|both
```

## ファイルの地図

| ファイル | 持つもの |
|---|---|
| `main.go` | 引数の解析 (Python の argparse の再現)・シグナルと中断 (`appCtx`)・`Env` (テストで差し替える口) |
| `script.go` | 台本の読み込みと検証・キャラの設定・合成のキャッシュの鍵 (`Params` / `cacheSerialize`)・読み替え |
| `synth.go` | エンジンへの要求 (audio_query / synthesis)・キャッシュ (`<台本>.work/`)・`kana` |
| `engine.go` / `engine_auto.go` | エンジンのコンテナの起動・停止 / 自動起動と、使われなくなったら止める見張り |
| `build.go` | 組み立て (`assemble`: wav の連結・口の開き・プレイヤーのデータ)・`cmdBuild`・HTML への埋め込み |
| `mp4.go` | mp4 の書き出し (プレイヤーのまとめ撮りを Chrome で撮り、ffmpeg で切り分けてつなぐ) |
| `show*.go` | 台本の `show` (図解: keyword / compare / code / image / mermaid) |
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
