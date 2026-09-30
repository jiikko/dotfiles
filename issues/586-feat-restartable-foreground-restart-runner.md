# 586 (feat): restartable — 前面で動かすコマンドを R で再起動・Q で終了できるランナー (他 repo から go install で使う)

> 🚨 **担当中: SFTP issue checkpoint [59ecdd]**（2026-09-30〜）

起票日: 2026-09-30

## 概要

obaket の `make dev-fg-loop` (`apps/obaket/macOS/bin/dev-fg-loop`) は、アプリをビルドして前面で起動し、Claude が `bin/dev-restart` から
再ビルド + 再起動できるようにしたループ。人がキーで再起動・終了できない。ユーザーの依頼 (2026-09-30):

> コマンドのforegroupプロセスでrestartする文字を受け付けてほしい. Rを押したら、そのばでrestartして欲しい. それとQでプロセスを終了して欲しい.
> 行末に常に選択肢を表示をして、ログは今まで通りtailする感じにして。

同じことを他の repo でもやりたいので、dotfiles の `src/` に汎用の Go のツールとして作り、各 repo は GitHub の URL から `go install` して使う
(ユーザー判断: 依存が増えてよい / GitHub の URL で参照する / bubbletea と `tuikit/confirm` を使う / ログは 0.5 秒程度バッファしてよい)。

## 要件

- R1 起動: `restartable [flags] [--build <cmd>] -- <run cmd...>`。ビルドが成功したら run を起動する。ビルドに失敗したら起動せず (古い成果物を起動しない) 待つ
- R2 最下行に常に選択肢と状態を出す。見た目 (ユーザー合意): `── [R] 再起動  [Q] 終了 ──────────────── running (pid 12345)`。状態は building / running / build-failed / stopping
- R3 R を押したら確認ダイアログ (`tuikit/confirm`) を出し、y で「止める → ビルド → 起動」。ビルド中の R は無視して「ビルド中」と出す。build-failed のときの R は再ビルド
- R4 Q を押したら確認ダイアログを出し、y で子を止めて終了 (rc 0)
- R5 Ctrl-C は子を止めて終了 (rc 130)。どの終わり方でも端末の状態を元に戻す
- R6 子の stdout / stderr は最下行の上へ流す。最大 0.5 秒ほどまとめて流してよい (大量のログで遅くしない)
- R7 子が自分で終わった (人の Cmd+Q 等) ときは、再起動の要求が無ければ終了する (今の dev-fg-loop と同じ)
- R8 外からの再起動: `restartable restart --control <socket>` で、動いているインスタンスに R と同じ要求を送る (確認ダイアログは出さない)。
  `restartable status --control <socket>` で識別子・状態・子の pid・世代を返す。obaket の `bin/dev-restart` はこれを使う
- R9 止め方: `--stop-cmd <cmd>` (obaket なら debug API の quit) → 猶予 → 子のプロセスグループへ SIGTERM → 猶予 → SIGKILL
- R10 `--id-env <NAME>`: インスタンスの識別子を子の環境変数に渡す (obaket の `devLoop` の照合 = 別のアプリを止めない仕組みを残す)
- R11 stdin が端末でないとき (テスト・CI) はキー入力と最下行を出さず、ログを素通しする。control socket は同じく効く
- R12 `go install github.com/jiikko/dotfiles/src/restartable@<版>` が dotfiles の checkout 無しで通る (module path を GitHub の URL にし、`replace` に頼らない)

## 受け入れ条件

- [x] M1: `termsafe` と `tuikit` の module path を `github.com/jiikko/dotfiles/src/termsafe` / `.../src/tuikit` に変え、取り込んでいる module
      (doctor / glogx / pro-con / ratelimit / schedkeys) の import と `replace` を追従させる。全 module の `make lint` / `make test` が通り、`bin/` のラッパーがビルドできる
- [ ] M2: `src/restartable` を 3 点セット (Makefile の lint / test、go.mod、`.github/workflows/src_restartable.yml`) と README 付きで足す。R1〜R11 をテストで固定する
      (キーと確認ダイアログは model の Update を直接叩く、プロセスの止め方と control socket は実プロセスで)
- [ ] M3: push 後、空の GOPATH / GOMODCACHE で `go install github.com/jiikko/dotfiles/src/restartable@<commit>` が通る (R12)
- [ ] 見た目 (最下行と確認ダイアログ) と R / Q の操作を、ユーザーが実端末で確かめる (human issue を起こす)
- [ ] obaket 側の切り替え (dev-fg-loop / dev-restart) は obaket に別の issue を起こす

## 進捗

- 2026-09-30 起票
- 2026-10-01 M1 完了
  - module / import / require / replace と package path の説明を GitHub path に統一。各 `replace` は従来どおり `../termsafe` / `../tuikit` を参照。
  - 7 module の `go mod tidy` はすべて成功。7 module それぞれの `make lint` / `make test` は最終実行で rc=0。
  - 指定の旧 path grep は 0 件（grep rc=1）。`GO_AUTOBUILD_SYNC=1 bin/glogx --help` は再ビルド後に rc=0。`pro-con help` / `schedkeys --help` / `svcdoctor --help` も rc=0。
  - codex は `doctor/internal/displaycheck` に termsafe の別名解決を足したが、Claude が使い捨ての worktree で元の版に戻して doctor の test を回すと rc 0 だった
    (package 名は termsafe のまま) ので戻した (commit「revert(doctor): displaycheck の termsafe 別名の解決を戻す」)。glogx の初回 race test は時間・allocation 閾値の失敗が出たが、単独再実行では rc=0 (codex の報告)
  - Claude の確認: 7 module の `make lint` / `make test` を回し直して全部 rc 0 (各 test ログに ok 行があり FAIL 0)、旧 import path の grep 0 件
  - commit「refactor(src): termsafe と tuikit の module path を github.com/jiikko/dotfiles/src/... にする (issue 586 M1)」
  - ratelimit の `--help` は usage を表示して rc=2（現在の `run` が flag parse error を usage code に写す）。
