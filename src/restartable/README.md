# restartable

`restartable` はコマンドを前面で起動し、端末のキーまたは Unix control socket から再起動・状態確認できる runner です。
`--build` を指定すると、ビルド成功後にだけ run コマンドを起動します。TTY の UI は stdin と stdout の両方が端末のときに表示します。

## インストール

GitHub のタグまたはコミットを版に指定します。

```sh
go install github.com/jiikko/dotfiles/src/restartable@<版>
```

## 起動

```sh
restartable --build 'go build -o ./bin/server ./cmd/server' -- ./bin/server --foreground
```

`--build` と `--stop-cmd` は `/bin/sh -c` で実行します。`--` の後の run コマンドは shell を介さず argv のまま起動します。
ビルドが失敗した場合は `build-failed` の表示で待ち、古い成果物は起動しません。`R` で再ビルドできます。

`--ready-cmd '<command>'` を指定すると、run コマンドを起動した後に確認コマンドを `/bin/sh -c` で実行します。rc 0 になるまで 0.5 秒間隔で繰り返し、1 回の実行は 5 秒で打ち切ります。既定の全体上限は `--ready-timeout 120s` です。上限までに確認できなくてもアプリは終了させず、`status` の `ready` は `false` のままになります。`--id-env NAME` を併用すると、確認コマンドにも run / build / stop command と同じインスタンス ID を渡します。

obaket では health の `devLoop` が現在の `$OBAKET_DEV_LOOP` と一致するまで起動確認を続けられます (health の URL は環境に合わせてください)。

```sh
restartable --id-env OBAKET_DEV_LOOP \
  --ready-cmd 'curl -fsS http://127.0.0.1:8080/health | jq -e --arg id "$OBAKET_DEV_LOOP" ".devLoop == \$id" >/dev/null' \
  --build 'make build' -- ./bin/obaket
```

TTY の最下行にはキーと状態が表示され、子の stdout / stderr はその上に流れます。

```text
── [R] 再起動  [Q] 終了 ──────────────────────────────────── running (pid 12345)
```

端末幅が狭い場合は罫線とキー表示が短くなります。確認中は `tuikit/confirm` の板が最下行より上に出ます。
起動時・再起動中・終了中は別の進捗板を端末の中央に表示します。起動時と再起動中は「終了 → ビルド → 起動 → 起動の確認」の現在段を spinner で示し、`--ready-cmd` の確認が済むまで板を残します。ビルド失敗と起動未確認は結果を表示して板を閉じます。終了中は子が終了するまで表示します。端末が小さい場合は板を最下行の直上に寄せます。幅10桁未満では板を出さず、段だけを1行で表示します。確認ダイアログも同じ幅では `終了y/n` / `再起動y/n` の1行にします (入らない幅では `y/n`)。

進捗板の段ごとにキー操作が異なります。すべての進捗段で `R` を受け付けず「処理中」と表示します。ビルド段と起動の確認段では `Q` で終了確認を表示します。終了の待ち段では `R` / `Q` を受け付けず「処理中」と表示します。`--stop-cmd` 成功後の終了待ちでは `Esc` で取り消せます。Ctrl-C はいつでも強制終了です。
`--ready-cmd` を省略した場合は子の起動直後に `ready: true` になります。

アプリ固有の quit API を使う場合は `--stop-cmd` を指定します。

```sh
restartable --build 'make build' --stop-cmd 'curl -fsS -X POST http://127.0.0.1:8080/quit' -- ./bin/server
```

`--stop-cmd-timeout` は stop command 自身の上限 (既定 5 秒)、`--term-grace` は強制終了経路で SIGTERM から SIGKILL まで待つ時間 (既定 5 秒) です。
既定の control socket は canonical cwd から決まります。

## キー

| キー | 動作 |
| --- | --- |
| `R` | 通常の running 中は再起動確認を表示。`y` / Enter で停止してビルドし直す。`build-failed` 中は確認なしで再ビルド。進捗板のすべての段では無視して「処理中」と表示。 |
| `Q` | 通常の running 中は終了確認を表示。ビルド段と起動の確認段では終了確認を表示し、`y` / Enter で終了。進捗板の終了の待ち段と起動段では無視して「処理中」と表示。 |
| `y` / `Y` / Enter | 確認中の操作を実行。 |
| `n` / `N` / Esc | 確認をキャンセル。 |
| Esc | `--stop-cmd` 成功後、子の終了待ち中なら停止を取り消して running に戻る。 |
| Ctrl-C | 確認状態に関係なく子のプロセスグループを強制終了し、runner を終了 (rc 130)。 |

## 停止の順序

- `--stop-cmd '<command>'` を指定した場合、R / Q / control restart は stop command を実行します。成功後はシグナルを送らず、アプリ自身が終了するのを待ちます。終了確認をアプリ側で出す構成に使えます。
- stop command が失敗するか `--stop-cmd-timeout` を超えた場合は子へシグナルを送らず running に戻り、エラーを表示します。
- stop command の成功後、子の終了待ち中に Esc を押すと停止を取り消します。Ctrl-C は待ちを中断し、プロセスグループへ SIGTERM、`--term-grace` 後に SIGKILL を送ります。
- `--stop-cmd` がない場合は、R / Q / control restart でもプロセスグループに SIGTERM を送り、猶予後も残っていれば SIGKILL を送ります。
- runner への外部 SIGINT / SIGTERM も強制終了経路です。SIGINT は rc 130、SIGTERM は rc 143 で終了します。

## control socket

runner を起動したディレクトリと同じディレクトリから実行すると、既定 socket を使って状態確認や再起動を依頼できます。

```sh
restartable status
restartable restart
```

出力には状態、子 PID、generation、起動確認済みかを示す `ready` が含まれます。runner と client で別の socket を使う場合は、両方に同じ `--control PATH` を指定します。
`--id-env NAME` を指定すると、生成したインスタンス ID を build / run / stop / ready command の環境変数に渡します。

## TTY UI を表示しない起動

stdin と stdout のどちらか一方でも端末でなければ、キー入力と最下行 UI は有効になりません。control socket は利用できます。子のログは、stdout が端末でなければ (パイプ・ファイル) byte のまま素通しします。stdout が端末なら (stdin だけをリダイレクトした場合)、TTY UI と同じく行に分けて端末の制御列を取り除いてから 1 行ずつ出します (色の SGR は残します。改行の無い末尾は改行か子の出力の終わりまで出ません)。子の stdin は UI の有無ではなく runner の stdin で決まります。runner の stdin が端末なら、TTY UI を表示しない場合も build / run / stop-cmd の stdin は `/dev/null` です。これにより、出力をパイプへ流したときに背景プロセスグループの子が端末入力で停止するのを防ぎます。runner の stdin が非端末なら、その入力を子へ渡します。
非 TTY では進捗板を描かず、`--ready-cmd` の確認結果を stderr に 1 行ずつ出します。
例えば CI や pipe では次のように実行します。

```sh
restartable --build 'make build' -- ./bin/server 2>&1 | tee server.log
```

この例は runner の stdin が端末、stdout がパイプなので headless で動きます。子は `/dev/null` から stdin を読みます。子にファイルやパイプの入力を渡す場合は、runner の stdin をリダイレクトしてください。

この判定のために `/dev/tty` を別途開くことはありません。

## 検出しない形 / 注意

- `RUNEWIDTH_EASTASIAN=1` のように曖昧幅の文字を 2 桁と数える設定では、罫線と `→` を含む進捗板や最下行が端末幅を超える場合があります (既存の最下行と同じ制約です)。
- `--control PATH` の親ディレクトリの所有者・権限は検査しません。自分だけが書けるディレクトリを指定してください。他人が書けるディレクトリでは socket を差し替えられ、`status` / `restart` が偽の応答を受けたり、runner へ届かなかったりします。既定の置き場所 (`$TMPDIR/restartable-<uid>`) は自分が所有する 0700 であることを確かめています。
- 既定の control path では checkout ごとに runner は 1 つですが、同じ checkout でも異なる `--control PATH` を指定すれば別の runner を起動できます。複数起動する場合は各 runner に異なる path を指定してください。
- runner の stdin が端末の場合、build / run / stop-cmd の stdin は `/dev/null` です。TTY UI の有無とは関係なく、子コマンドから端末の対話入力はできません。runner の stdin が非端末なら、その入力を子へ渡します。
- build 中に Ctrl-C で強制終了した場合、前世代の子が残した process group の子孫まで検出して停止する保証はありません。
