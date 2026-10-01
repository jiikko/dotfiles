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

TTY の最下行にはキーと状態が表示され、子の stdout / stderr はその上に流れます。

```text
── [R] 再起動  [Q] 終了 ──────────────────────────────────── running (pid 12345)
```

端末幅が狭い場合は罫線とキー表示が短くなります。確認中は `tuikit/confirm` の板が最下行より上に出ます。

アプリ固有の quit API を使う場合は `--stop-cmd` を指定します。

```sh
restartable --build 'make build' --stop-cmd 'curl -fsS -X POST http://127.0.0.1:8080/quit' -- ./bin/server
```

`--stop-cmd-timeout` は stop command 自身の上限 (既定 5 秒)、`--term-grace` は強制終了経路で SIGTERM から SIGKILL まで待つ時間 (既定 5 秒) です。
既定の control socket は canonical cwd から決まります。

## キー

| キー | 動作 |
| --- | --- |
| `R` | running 中は再起動確認を表示。`y` / Enter で停止してビルドし直す。`build-failed` 中は確認なしで再ビルド。building 中は処理せず「ビルド中」と表示。 |
| `Q` | 終了確認を表示。`y` / Enter で runner を終了。 |
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

出力には状態、子 PID、generation が含まれます。runner と client で別の socket を使う場合は、両方に同じ `--control PATH` を指定します。
`--id-env NAME` を指定すると、生成したインスタンス ID を build / run / stop command の環境変数に渡します。

## TTY UI を表示しない起動

stdin と stdout のどちらか一方でも端末でなければ、キー入力と最下行 UI は有効になりません。ログは従来どおり stdout / stderr に素通しし、control socket は利用できます。子の stdin は UI の有無ではなく runner の stdin で決まります。runner の stdin が端末なら、TTY UI を表示しない場合も build / run / stop-cmd の stdin は `/dev/null` です。これにより、出力をパイプへ流したときに背景プロセスグループの子が端末入力で停止するのを防ぎます。runner の stdin が非端末なら、その入力を子へ渡します。
例えば CI や pipe では次のように実行します。

```sh
restartable --build 'make build' -- ./bin/server 2>&1 | tee server.log
```

この例は runner の stdin が端末、stdout がパイプなので headless で動きます。子は `/dev/null` から stdin を読みます。子にファイルやパイプの入力を渡す場合は、runner の stdin をリダイレクトしてください。

この判定のために `/dev/tty` を別途開くことはありません。

## 検出しない形 / 注意

- 既定の control path では checkout ごとに runner は 1 つですが、同じ checkout でも異なる `--control PATH` を指定すれば別の runner を起動できます。複数起動する場合は各 runner に異なる path を指定してください。
- runner の stdin が端末の場合、build / run / stop-cmd の stdin は `/dev/null` です。TTY UI の有無とは関係なく、子コマンドから端末の対話入力はできません。runner の stdin が非端末なら、その入力を子へ渡します。
- build 中に `Q` → `y` で強制終了した場合、前世代の子が残した process group の子孫まで検出して停止する保証はありません。
