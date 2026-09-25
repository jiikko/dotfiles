# 487 (feat): レビューの列に来たカードを、dispatcher が起こす「取り込みの係」の session に知らせる

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26): 「想定している通り、レビューレーンを消化するように PM 周りの挙動を修正して」。
どちらの形にするかを聞き、ユーザーは「取り込みの係を別の自動 session にする」を選んだ (PM にレビューもさせる案・人が手でやる案ではなく)。

437 の分担 (2026-09-25 のユーザーの決定「両方: 依頼の分解だけ自動」) では、レビュー・差し戻し・完了・master への取り込みは
「取り込みの係 (人間か、人間の代わりの Claude の session)」の仕事で、PM は触らない (`pm-guide.md` の役目 5)。
ところがその係は誰も自動で起こしていない。クラッシュ前は手で開いた session が務めていて、2026-09-25 23:53 のクラッシュで消えた後、
レビューの列に 8 枚が溜まり、`--after` で後ろに積んだ 5 枚も起動できなくなった (485 の実例)。

## 設計

PM の仕組み (`src/pro-con/dispatcher/pm.go`。437 で失敗モードを固め、変異 21 本で守っている) を**役 (role) の引数で回す形に括り出し**、
PM と取り込みの係の 2 つを同じ機構で動かす。取り込みの係の分を複製しない (同じ状態機械が 2 つになると、片方だけ直す形が必ず出る)。

| | PM (今のまま) | 取り込みの係 (新しい役) |
|---|---|---|
| 起動の記録のカード ID | `PM` | `INT` (`C-%03d` と重ならない) |
| 様子のファイル | `pm.json` | `integrator.json` (中身は同じ型) |
| 知らせる物 (鍵) | 依頼の列のカード (ID) / PG の質問 (ID@質問待ちに入った時刻) | レビューの列のカード (ID@レビューに入った時刻。差し戻して戻ったらまた知らせる) |
| 指示書 | `pm-guide.md` (`pro-con card guide`) | `integrator-guide.md` (新設。`pro-con card guide --integrator`) |
| worktree の名前 | `pc-pm-<時刻>` | `pc-int-<時刻>` |
| 止める設定 | `--pm=off` / 設定 `pm = "off"` | `--integrator=off` / 設定 `integrator = "off"` |

- 括り出すもの: `tellPM` / `preparePM` / `pmAdopt` / `registerPM` / `stopPM` / `settlePM` と、Dispatcher の `pmHeld` / `pmFailed` / `pmStatus` / `pmRejects` / `pmRejected`
  (役ごとの持ち物にする)。PM の出来事の文言・挙動は変えない (既存の PM のテストが括り出しの守りになる)
- どちらも `--limit` (PG の数) に数えない。利用枠で「新しく起動・再開しない」(95%) ときだけ起こさない (PM と同じ)
- 終了 (Shutdown) は両方の役を止める。`pscmd.go` の一覧は `PM` / `取り込み` の行を出す。`shutdown.go` の `stopName` も役から引く
- 起こす repo は PM と同じ (`pm_repo`。今は dotfiles)。取り込む先はカードの repo で、係はその repo に自分用の worktree を作って merge する (指示書に書く)
- e2e モード (`PMRepo` が空) では起こさない

### 指示書 (`integrator-guide.md`) に書くこと

- `pm-guide.md` の役目 5 (レビュー・差し戻し・完了) をこちらへ移し、pm-guide には「取り込みの係が扱う。PM は触らない」だけ残す
- 手順: カードの PG の worktree とブランチを `pro-con card show` で読む → origin/master から自分用の worktree を作って merge → diff を読む →
  pro-con の `make test` / `make lint` → よければ `git push origin HEAD:master` して `pro-con card close <カード> --issue <repo>#<番号>`
- **衝突したら自分で解かず、`pro-con card rework` で PG に戻す** (両方を残せば済む文書の衝突は解いてよい。コードの衝突は PG が直す)。
  2026-09-26 に手で取り込んだとき、C-022 / C-030 / C-036 は master で名前の変わった関数に合わせる書き直しが要った
- PG の「終わった」は証拠ではない (415 論点 4)。`--after` の付いたカードは先に入ったカードと合わせた結果をテストで見る (468)

## 決めたこと / 決めていないこと

- 決めた: 取り込みの係は 1 つ。PG の枠に数えない。自動で master へ push する (人の確認を挟まない。ユーザーが自動の係を選んだ)
- 未確認: bg の session (`--setting-sources project,local`) で `git push origin HEAD:master` が権限の確認で止まらないか。
  止まったら status が waiting のまま動かないので、dispatcher の watchdog / 様子で分かる形にする (今の PM と同じ見え方)
- 決めていない: 取り込みの係の数を設定で増やす (456 の `pm` と同じく、今は 1 だけ)

## 関連ファイル

- `src/pro-con/dispatcher/pm.go` / `dispatcher.go` の `tick` と Dispatcher の欄 / `shutdown.go` の `stopPM` の呼び出しと `stopName`
- `src/pro-con/store/pm_state.go` / `src/pro-con/pscmd.go` / `src/pro-con/dispatchercmd.go` (`--integrator`) / `src/pro-con/config/config.go`
- `src/pro-con/pm-guide.md` / `src/pro-con/cardcmd.go` (`card guide`)

## 関連

- 437 (PM を起こす仕組みと分担) / 485 (依存の実例) / 468 (`--after`) / 452 (人の番の目印)

## 進捗

- [ ] 設計のレビュー
- [ ] PM の仕組みを役の引数に括り出す (PM の挙動は変えない。既存のテストが green のまま)
- [ ] 取り込みの係の役を足す (鍵・知らせ・指示書・止める設定・一覧)
- [ ] テスト (レビューの列に来たら知らせる / 差し戻して戻ったらまた知らせる / 終了で止める / PM と記録の行が混ざらない / off) と変異
- [ ] 敵対的レビュー
- [ ] 本番の dispatcher で、レビューの列のカードに取り込みの係が起きることを確かめる
