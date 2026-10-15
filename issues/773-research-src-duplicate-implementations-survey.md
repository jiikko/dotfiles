# 773 (research): src の重複実装の洗い出し (2026-10-15)

起票日: 2026-10-15

## 概要

ユーザー依頼 (2026-10-15): 「src のうち、重複している実装がないかを調べて issues に書き出して」。766 (spinner の字形の 4 重複を tuikit に寄せた) の続き。
read-only のサブエージェント 1 体 (opus) が洗い出し、上位の主張は main が本文で確かめた。この issue は調査の記録 (母集合・切り出した issue・低価値の候補・却下) を持つ。

## 母集合の数え方

src の 22 module の非テストの `.go` を観点ごとに grep した。観点と式は次のとおり。

| 観点 | grep の式 |
|---|---|
| 幅と ANSI | `func .*strip.*(ansi\|sgr)` / `ansi.Strip(` / `\x1b[` |
| アトミックな書き込み | `os.Rename(` / `os.CreateTemp(` |
| ロック | `syscall.Flock` / `O_EXCL` |
| プロセス | `Kill(-` / `Setpgid\|Setsid` / `"ps"` |
| 時間の上限 | `WaitDelay` |
| 外部コマンド | `"pbcopy"` / `"open"` |
| 整形 | `func .*(Size\|human\|Ago\|Dur\|age)` |
| 無害化 | `sanitize*\|clean*` / `unicode.IsControl` |
| パス | `func .*(under\|within)` / `Getenv("XDG_*")` / `".config"` |
| テストの待ち | `_test.go` の `^func (waitFor\|waitUntil\|eventually\|pollUntil…)` |
| fake clock | `type .*Clock` |

候補ごとに、別実装の理由を書いたコメントがないかを grep し、重なる issue を `grep -rln <symbol> issues` で探した。
寄せないと決め済みの issue 716 (done) の表に載っているものは除いた。

## 切り出した issue (価値の高い順)

- 767: pro-con の一時ファイル + rename の 5 か所を atomicfile に寄せる (固定名 `.tmp` の 2 か所は取り合いが起きうる)
- 768: pro-con の presence と relay の「flock した印」の作り方と掃除を 1 か所にする
- 769: pro-con の UI の pbcopy と open を subproc に寄せる (issue 718 の取りこぼし)
- 770: Go が手で写しているテーマの色番号を、theme/colors.yml との一致の検査に入れる
- 771: doctor の 4 package の行の無害化を termsafe.PlainLines に 1 つにする
- 772: pro-con の経過時間の書式と、CLI と画面で別々に組んでいる同じ行をそろえる (見た目の決定が要る)

## 切り出さなかった候補 (価値が低い・寄せる費用が大きい)

着手するなら、この節から issue を起こす。

- **プロセスグループを止める処理が 5 実装ある**
  - 場所: `pro-con/dispatcher/runner.go` / `restartable/internal/runner/process_unix.go` の `signalGroup` / `doctor/runner/runner.go` の `Exec` / `lockman/with.go` の `killGroup` / `proctree`
  - 止め方の方針 (TERM → KILL・lease) は意図的に違う
  - 共有する価値があるのは「pgid が 1 以下なら撃たない」(`kill(-1)` は全プロセス宛て) の守りだけ。これを持つのは lockman と pro-con の readPgid の経路だけで、
    restartable は `pid <= 0` しか弾かない。ただし restartable が渡すのは `cmd.Process.Pid` なので、1 が来ることは無い見込み (未確認)。
  - restartable は下の却下の理由で寄せられない
- **テストの「条件を待つ」helper が約 11 個ある**
  - 場所:
    - pro-con: `cardview_test.go` の `waitUntil`、`wake` / `relay` の `waitFor`、`dispatcher/execrunner_test.go` の `pollUntil`
    - その他: zundamon の `waitUntil`、runtimeout / proctree / treefiler の `waitFor`、process_supervisor の `eventually`、lockman の `waitForCondition`、restartable の `waitFor`
  - module をまたいで寄せると、新しい module・replace・CI の paths filter が要る。安いのは pro-con の中の 4 つを pro-con 内で 1 つにすることだけ
- **人が読むサイズの整形が 4 つある** (どれも 1024 進)
  - `pro-con/diskuse.Human` ("1.5G")
  - `doctor/disk.HumanSize` ("1.5GB")
  - treefiler の `filer/model.go` の `human` ("1.5 GB") と `shortSize` (4 桁の欄用)
  - 揃えるのは見た目の決定になる
- **`~/.config/glogx` の組み立てが 2 か所にある**
  - `treefiler/filer/settings.go` の `configDir` と `mac-lan-manager/ledger` の `DefaultPath`
  - キャッシュの置き場には `doctor/cachedir` があるが、設定の置き場に対応する package は無い
- **`treefiler/filer/preview.go` の `cleanLine`**
  - `PlainLineKeepTabs` を通してからタブを空白 4 つに置き換えている。`termsafe.PlainLine` と等価に見えるが、総当たりでは確かめていない (未確認)
- **ruleguard の規則の写しに、ずれの検査が無い**
  - 写し: `padViaPadSpaces` が 3 module (glogx は Report の文言だけ違う)、`execViaForeground` が 2 module
  - dsl を解決できないので、写すこと自体は避けられない
  - ずれを検出する検査が無い (tests / scripts / Makefile に gorules の参照が 0 件。未確認の粗い数え)。`_claude/rules/claude-md-maintenance.md` の「コピーするなら検出を置く」に当たる
- **rune の境界でバイト数を切るループ**
  - `doctor/svc/restore.go` の `cleanText` と `glogx/doctor_cache.go` の `cutRunes` が同じ 4 行

## 却下 (意図的、または同じ働きではない)

- **ANSI の剥がし**: glogx の `stripANSI` と schedkeys の `stripSGR` は `termwidth.StripSGR` の別名。pro-con の `ansi.Strip` の直呼び 4 か所は同じ parser を使っているだけで、別の実装ではない
- **zundamon の `outfile.WriteAtomic` / `ReplaceKeepingMode`**: Python 版とパーミッションの意味を揃えるため (コメントにある。敵対的レビュー P2-1)
- **`doctor/disk/delete.go` の履歴ファイル**: O_EXCL の名前の規約が違い、atomicfile の doc 自身がそれに触れている
- **docker の `HumanSize` と ssd の `decimalBytes`**: 外部ツールの出力と突き合わせるために 10 進にしている (issue 254)
- **キャッシュ 3 つ** (glogx の `usage_cache` / ratelimit の `-cached` / `usage/shared.go`): 鮮度の契約が違う (`ratelimit/main.go` の冒頭)
- **schedkeys の toast**: 罫線と ✓ が ambiguous 幅で、IME の位置がずれる (`toast.go` の冒頭)
- **restartable**: y/N の規則が違う (`runner/model.go` のコメント)。go install する module なので、ローカルの module (`proctree` / `subproc` / `atomicfile`) を import できない
- **`doctor/disk/main_test.go` の sandbox と `safefs/sandbox`**: safefs/sandbox の doc に「振る舞いを変えないため寄せていない」とある
- **パスの包含判定**: `pro-con/wtclean/judge.go` の `within` は大文字小文字を無視するのが意図 (APFS)。`dispatcher/runner.go` の `isUnder` は Rel で比べる。用途が違う
- **pid の生死判定** (chromecookie の `processAlive` / `doctor/testtmp` の `alive` / dispatcher の `pidAlive` / store): EPERM や判定できないときをどちらに倒すかが、用途ごとに意図的に違う
- **flock の確かめ**: zundamon の `InUse` と `ReaperAlive` は、失敗したときの倒し方が意図的に逆
- **pro-con の `ps` の解析 5 か所と `proctree.listProcs`**: 取る列と目的が違う (寄せられるかは未確認)
- **issue 716 で寄せないと決めたもの**: 入力欄・スクロールバー・ExecRequest・hint・porcelain など

## 進捗

- [x] 洗い出し (2026-10-15)
- [x] 上位の主張を本文で確かめた。確かめた内容:
  - 767 の 5 か所と固定名の 2 か所
  - 768 の `staleTmp` の重複
  - 769 の `pbcopy` / `open`
  - 770 の生成を畳んだ注記と、検査の対象
  - 771 の 4 関数
  - 772 の `fmtAge` / `fmtDur` の書式
- [ ] 反証レビュー
