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

- [x] 設計のレビュー (opus 1 本)。採った指摘: 入力待ちで止まった係が誰にも見えない (P1) / 係が片付けないカードで起こし直し続ける (P2。レビュー待ちでも
  `card handoff` を受け、人に回したカードを知らせる物から外す) / 追加オーダー・push の衝突・merge の残骸 (P2。指示書の手順に書いた) / 古いコメント 3 か所。
  却下: 「bg の session では push が権限の確認で止まる見込み」— PM は同じ起動条件で既に issue の commit を push している (2026-09-26 01:21 の
  「486 に PM の順番の見積もりを書く」ほか) ので、push そのものは止まらないと見る。止まったときは入力待ちの出来事で分かる
- [x] PM の仕組みを役の引数に括り出す: `dispatcher/role.go` (tellRole / prepareRole / adopt / registerRole / stopRole / settleRole)。
  役ごとの持ち物は `roleRun`。PM の文言・挙動は変えていない (既存の PM のテストが無修正で green。変えたのは `d.pmRejects` の参照 1 か所と `pmRow` の削除だけ)
- [x] 取り込みの係の役 (`dispatcher/integrator.go`。カード ID `INT`、鍵 ID@レビューの列に入った時刻、人に回したカードは外す)・`integrator.json`・
  `--integrator` / 設定 `integrator` (on / off 以外は誤り)・`pro-con card guide --integrator` と `integrator-guide.md`・pm-guide の役目 5 を縮めた・
  `pro-con ps` の「取り込み」の行・入力待ちを 1 度だけ出来事にする (PM にも当たる)
- [x] テスト 12 本 (dispatcher 9・config 1・指示書 2) と変異: 役を回さない / 鍵に時刻を入れない / 人に回したのを外さない / store がレビュー待ちの handoff を断る /
  off を無視する / 終了で PM だけ止める / 役ごとの終了の段で 1 つ目の役で抜ける / 指示書を取り違える / 入力待ちの出来事を消す・抜けても外さない・
  生きていない Tick で外さない / 入力待ちを知らせる物のある分岐でしか出さない / config の検査を消す — の 14 本すべて red
- [x] 敵対的レビュー 2 周 (opus)。1 周目の採用: config の検査漏れ / 入力待ちが「知らせる物があるとき」しか出ない / 括り出しで PM の「知らない status」の
  出来事が 1 回で済まなくなった / 指示書の例が自分の規則と矛盾 / handoff の --from の既定 / テストの穴 3 つ。2 周目の採用: 起こし直した session が
  busy を見せずにまた入力待ちになると出ない (生きていない Tick で印を外す) / テストが戻り値を捨てていた。2 周目の修正は判断を新設せず変異で直に確かめたので打ち切り
- [x] 本番の dispatcher で、レビューの列のカードに取り込みの係が起きることを確かめる (2026-09-26: 係が 488 を取り込んで閉じ、08:28 に C-046 のレビューで再開されて 08:34 に閉じた。`pro-con log` の INT の出来事)

## 残り

- 未確認: 係が自動で master へ push した結果を、人が後から見る口 (今は `pro-con log` と git log だけ)
- 未テスト: `pro-con ps` の「取り込み」の行 (ps に既存のテストが無い。表示の 1 行)
- 係が生きていて close も rework も handoff もせずに turn を終えたカードは、知らせ済みなので再び知らせない (PM の依頼の列と同じ形。指示書の規律で持つ)
