# zundamon-kaisetsu (Go)

台本 JSON を VOICEVOX で合成し、四国めたんとずんだもんの掛け合い動画 (HTML プレイヤー / mp4) を作る CLI。
**使い方・台本の書き方は skill 側** ([`_claude/skills/zundamon-kaisetsu/SKILL.md`](../../_claude/skills/zundamon-kaisetsu/SKILL.md)・
[`README.md`](../../_claude/skills/zundamon-kaisetsu/README.md))。ここは実装を触る人向けの地図。

## 起動の経路

`bin/zundamon-kaisetsu` (go_autobuild のラッパー) が、skill のディレクトリを環境変数 `ZUNDAMON_KAISETSU_SKILL_DIR` で渡して起動する。
テンプレート (`templates/player.html`)・立ち絵の既定の置き場・略語の一覧 (`acronyms.json`。`kana --check` が読む) はそこから読む
(読み替えの辞書 `readings.json` は `lint` が合流漏れの検査にだけ読む。台本の `readings` へ合流させる手順は SKILL.md)。バイナリを直接起動するときも、この環境変数が要る。

```sh
zundamon-kaisetsu check | up | down | speakers | kana … | lint script.json | synth script.json | times script.json | view script.json | build script.json -o out --format html|mp4|both
```

## パッケージの地図

`main.go` (引数の解析 = Python の argparse の再現・シグナル・コマンドの振り分け) のほかは `internal/` のパッケージに分けている
(issue 741)。上の層は下の層だけを import する (循環はコンパイラが止める)。

| 層 | パッケージ | 持つもの |
|---|---|---|
| 0 | `pyjson` | Python 版と同じ値を作るための JSON・丸め・文字列化 (`Str` / `Repr` / `Round` …) |
| 0 | `textx` | 表示用の切り詰め (`FirstRunes` / `LastRunes`)・全角英数字の半角化・英字の語の切り出し |
| 0 | `app` | `Env` (テストで差し替える口)・利用者に見せる失敗 (`Fail` / `ErrExit`)・中断 (`Ctx` / `InterruptedErr`) |
| 0 | `paths` | パスの解決 (`ResolvePath`)・台本からの相対パス・作業 dir (`WorkDir` = `<台本>.work/`) |
| 0 | `wav` / `outfile` / `extcmd` | wav の入出力 / 出力ファイルの置き換え / 外部コマンドの探索 (Chrome・ffmpeg のエンコーダ) と時間の上限付きの実行 (`RunQuiet`) |
| 1 | `show` | 台本の `show` (図解: keyword / compare / list / code / image / mermaid) |
| 2 | `script` | 台本の読み込みと検証・キャラの設定 (`CastOrder`)・合成のキャッシュの鍵 (`Params` / `CacheSerialize`)・読み替え・`overlays` |
| 3 | `build` | 組み立て (`Assemble`: wav の連結・口の開き・プレイヤーのデータ)・`CmdBuild`・HTML への埋め込み・mp4 の書き出し (`mp4.go`: まとめ撮りを Chrome で撮り ffmpeg で切り分けてつなぐ)・行の時刻 (`line_times.go`: wav の長さと間から行の開始・終了を出す。synth の字幕の速さの警告と `times` が使い、`Assemble` と同じ値になることをテストで固定。issue 745)・字幕の速さの警告 (`caption_speed.go`)・まばたき (`blink.go`)・区切りのカード (`chapter_card.go`)・台本をユーザーに見せる一覧 `view` (`script_view.go`。issue 810)・ラベルの意図しない改行の検査 (`label_wrap.go`。Chrome の描画を player.html の `#measure` で測る。issue 812) |
| 3 | `engine` | エンジンのコンテナの起動・停止 (`engine.go`) / 自動起動と、使われなくなったら止める見張り (`engine_auto.go`) |
| 3 | `lint` | `lint`: 台本の校正のうち文字列と構造だけで決まるもの (`lint.go`。issue 675 / 686。試験用の台本は skill の `examples/review-bench/`)・`lint --source 資料` の突き合わせの候補 (`source_check.go`。issue 685) |
| 4 | `synth` | エンジンへの要求 (audio_query / synthesis)・キャッシュ・`kana` (`synth.go`)・`kana --script --check` の英字の語の警告 (`reading_check.go`。略語の一覧は skill の `acronyms.json`。issue 673) |

テストは全部 main に置いている (各パッケージへは移していない): `testdata/` と skill のディレクトリを cwd からの相対パスで読み、
mp4 の偽の Chrome / ffmpeg はテストのバイナリ自身を起こし直し (`mp4_test.go` の `TestFakeMP4Tool`)、共有の偽物 (`helpers_test.go`) を
ほぼ全部のテストが使うため。テストから触る名前は公開名になっている。

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
