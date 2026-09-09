# bug: `_dotfiles_check` の結果ファイルが共有の固定名で、並行シェル間で所有権が入れ替わる

起票日: 2026-09-06
カテゴリ: bug
優先度: 中
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。3 エージェントが独立に検出

## 何が起きているか

`_zshrc:735-790` の `_dotfiles_check_*` は、非同期チェックの結果を
**`${XDG_CACHE_HOME}/dotfiles/${name}-check.result`** という**シェルをまたいで共有の固定名**に置く。
一方 `_dotfiles_check_pending` は**シェルごとのカウンタ**なので、母集合とカウンタが一致しない。

壊れ方は 2 経路ある。

**① 消費側が他シェルのぶんまで食う** — `_dotfiles_check_notify` の glob は
`*-check.result(N)` で、**自分が投げたぶんかどうかを見ていない**。
他シェルの result を読んで `rm` し、自分のカウンタを減らす。

```zsh
for f in "${XDG_CACHE_HOME:-$HOME/.cache}/dotfiles"/*-check.result(N); do
  msg="$(<"$f")"; command rm -f "$f"      # 誰のぶんかを見ていない
  (( _dotfiles_check_pending-- ))         # 自分のカウンタだけ減る
done
```

**② 生成側が他シェルの未読を消す** — `_dotfiles_check_watch` は bg 起動の**前に**
`command rm -f "$f"` する（:773）。新しいシェルを開くたび、先行シェルの未消費 result が 1 つ消える。

## 実害（過大に書かないこと）

- **通知そのものは失われない**のが主経路（消費した側のシェルには表示される）。
  ずれるのは**表示先のシェル**
- カウンタが 0 まで落ちないと `precmd` hook が解除されず**残り続ける**（1 プロンプトあたり
  readdir 1 回。シェル寿命で有界なので実害は小さい）
- 経路 ② では**通知が 1 つ落ちる**ことがある（これが medium を支えている根拠）
- 🚨 「`~/.claude` のリンク漏れを伝える唯一の経路」は**誤り**。同期の `_dotfiles_check_claude_links`
  （`_zshrc:800`）と SessionStart hook `claude-links-sync.sh` が別に持つ

## 推奨対応（3 点セット。部分適用は別の穴に化ける）

1. 結果ファイル名を **`${name}-check.$$.result`** へ
2. `notify` の glob を**自分の `$$` のぶんへ絞る**
3. `zshexit` で自分のぶんを掃く

さらに、カウンタを持ち回るのをやめて「**自分が登録したファイル名の配列が空になったら hook を外す**」
形にすると、母集合とカウンタの不一致が構造的に起きなくなる。

## 🚨 `$$` 化の副作用（着手前に設計を決めること）

今は固定名 2 本（setup / karabiner）なので**上限が構造的にある**。`$$` 化すると
「途中で死んだシェルぶんの result」が pid の数だけ溜まる = **リークを直す修正が別のリークを作る**。

- `zshexit` の掃除は「合わせて」ではなく**必須要件**
- それでも `zshexit` が走らない死に方（SIGKILL / 端末強制終了）が残る
- 掃除を足す時点で**破壊的操作の新設**なので、対象を `*-check.<digits>.result` パターンに限定する
  （[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §0-A）

## 🚨 採ってはいけない修正案（却下理由）

- **「pending カウンタをやめて result が 0 件であることを自己解除条件にする」**: そのまま実装すると
  **通知が永久に出なくなる**。`_dotfiles_check_watch` は bg 起動前に `rm -f $f` しており result が
  生えるのは `shasum` の後なので、起動直後の最初の precmd で 0 件 → 即 hook 解除。
  しかも「result を置いてから notify を呼ぶ」形のテストでは**緑のまま通る**（fixture が退行から
  不可視 = [`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md) の典型）。
  成立させるには「watch 登録時に pending marker を先に作り bg が上書きする」形が要る
- **「pid が生きていない古い `*-check.*.result` を掃除する」**: 報告された実害に対して重すぎ、
  新しい破壊的経路を 1 本増やすうえ pid 再利用の窓を持ち込む。掃除は `zshexit` に閉じる

## 検証

`$$` を差し替えた 2 つの偽シェルを立てて、①片方の result をもう片方が消費しないこと
②`watch` の登録が他方の未読を消さないこと、を見る。**変異検証**は glob の絞り込みを外すと red。

---

## 実測での訂正 (2026-09-09 / 着手前に検証)

| issue の記述 | 実測 |
|---|---|
| `_zshrc:735-790` | 実際は **767–806** (`_dotfiles_check_notify` / `_bg` / `_watch`)。約 32 行ずれ |
| `_dotfiles_check_watch` の `rm -f` は `:773` | 実際は **:801** |
| `_dotfiles_check_claude_links` は `_zshrc:800` | 実際は **:828** |
| 経路② で「先行シェルの未消費 result が **1 つ**消える」 | 消えるのは **watch 登録 1 回につき 1 本** = シェル起動あたり**登録している 2 本すべて**。実測: A が残した 2 本が B の起動直後に **0 本** |
| 「`zshexit` が走らない死に方 (SIGKILL / **端末強制終了**) が残る」 | **端末を閉じる = SIGHUP では zshexit は走る**。走らないのは **SIGKILL だけ**。SIGTERM / SIGINT では対話シェルはそもそも終了しない (zsh 5.9 / 対話 pty で各 3 回、3/3 で一致) |
| 「3 エージェントが独立に検出」 | **裏取り不能**。出典 commit `0b3dd21b` は forge Minimum+ (3 体 + クロスレビュー 2 体) と記録しており 3 体走ったこと自体は事実だが、この issue に 3 体全部が当たったかは生出力が `tmp/` にあったため確認できない (誤りとは言えない) |
| 「通知そのものは失われない」が主経路 | 概ね正しいが、下の④により**失われる経路がもう 1 本ある** |

現行実装で実測した壊れ方 (数値):

- **経路①**: watch を 1 件しか登録していないシェルが、他シェルのぶんを含め 2 件消費して `pending=-1`
- **経路③**: 食われた側は `pending=1` のまま `precmd` hook が **1 件残存**
- **④ (issue 未記載 / 今回見つけた)**: `print -r -- "$msg" > "$f"` は create と write が別操作なので、
  その隙間で `precmd` が拾うと**空を読んで「同期済み」と解釈し、通知を 1 つ落とす**。
  pid で分離すると「他シェルが食う」経路が消えるぶん、これが**通知が消える唯一の経路**として残るので同時に潰した

## §0-A の答え: 掃除機構は新設しない

採った形は「**掃除機構を作らない**」。`zshexit` が消すのは、このシェルが `_dotfiles_check_files` に
登録した**そのパス (と `.partial`)** だけで、ディレクトリを走査しない。よって
**母集合の限定・symlink を辿らない・uid 確認・名前からの pid 抽出が最初から要らない**
(`src/doctor/testtmp/testtmp.go` と `scripts/with_fresh_worktree.sh:sweep_stale` が必要としたものが全部不要。
数字判定が無いので `[0-9]` のロケール罠も出てこない)。

pid を見るのは 1 箇所だけ (下記の `kill -0 "$owner"`) で、それも**自分の親 1 つの生存確認**であって
他人のファイルを消す判定には使わない。誤れば「読まれないファイルが 1 つ残る」側にしか倒れないので、
`testtmp.go` が必要とした pid 再利用の慎重さ (消す側の判定) とは危険度が違う。

採らなかった案:

- **pid が死んでいる `*-check.*.result` を掃除する**: issue の却下どおり。母集合の走査 = 新しい破壊的経路
- **result ファイルをやめて「親が開いたまま unlink した fd」で渡す** (`zmodload zsh/system` + `sysseek`):
  カーネルが回収するので残骸は原理的にゼロになるが、**「bg がまだ書いていない」と「同期済み (空)」を
  区別できなくなる** (今はファイルの存在がその区別を担っている) うえ、起動時に module load が乗る。却下

🚨 **`zshexit` だけでは足りなかった (敵対的な見直しで発見し、同じ変更で塞いだ)**。
`zshexit` が消せるのは「終了時点で存在するもの」だけで、`_dotfiles_check_bg` は `&!` で
disown 済み = **親より長生きしうる**。親が bg の完走前に終わると、bg が後から result を公開して
**誰も読まない残骸が確定する**。しかもこれは pid 名にしたことで**新しく生まれる蓄積経路**で
(固定名の頃は次のシェルの `rm -f` が拾っていた)、issue が警告していた
「リークを直す修正が別のリークを作る」そのものだった。

実測 (2026-09-09): `shasum` を親の終了までブロックさせる shim を PATH 先頭に置いて再現。
`setup-check.<pid>.result` が親の死後に 1 本公開された。→ **公開の直前に `kill -0 "$owner"` で
親の生存を確認し、死んでいたら `.partial` を捨てる**形にした (`kill` は zsh builtin なので fork なし。
pid 再利用や EPERM を踏んでも被害は「読まれないファイルを 1 つ置く」側にしか出ない)。

残る穴は **SIGKILL** と、**`kill -0` から `mv` までのごく短い窓**だけ。しかも構造的に狭い:
`watch` は `watched -nt state` のときしかファイルを作らず (= dotfiles がずれている間だけ)、
作られたものは**次の precmd で消費される**。頻度は未実測 (件数を主張しない)。

## todolist

- [x] issue の主張を実測で確かめ、ずれを本文へ書き戻す
- [x] §0-A (掃除機構を作らずに済む構造) を先に問い、答えを残す
- [x] 結果ファイルを `${name}-check.$$.result` へ
- [x] 消費側を「自分が登録した配列」に変える (カウンタ廃止 = 母集合と数え手の不一致が構造的に消える)
- [x] `zshexit` で自分のぶんを掃く
- [x] ④ (非原子的な公開) を `.partial` → `mv` で潰す
- [x] ⑤ (自分で作った新しい蓄積経路: 親の死後に bg が公開する) を `kill -0 "$owner"` で潰す
- [x] 回帰テストを足し、変異を当てて red を確認する
- [x] `make test-zshrc` / `make test-lint` を通す

## 進捗

`fix(300): dotfiles check の result を pid 名にし、自分のぶんだけ消費する`
(`_zshrc` / `tests/zshrc/test_dotfiles_check_result_ownership.sh`)

- `typeset -g _dotfiles_check_pending` → `typeset -ga _dotfiles_check_files`
- `_dotfiles_check_notify`: ディレクトリの glob をやめ、自分が登録したパスだけを見る。
  受け取り切ったら (他シェルの未読が残っていても) 自己解除
- `_dotfiles_check_cleanup` を新設し `zshexit` に登録
- `_dotfiles_check_bg`: `> "$f"` → `> "$f.partial" && mv -f`
- `_dotfiles_check_watch`: ファイル名に `$$`、`.partial` も含めて自分のぶんを事前掃除、`zshexit` を登録

## 結果 (実測)

テストは 9 assert。ベースライン green を 3 回連続で確認 (並行性を含むため)。
**変異 8 本を当て、8 本とも red**。ケース別の pass/fail 一覧で判定した (スイートの rc では読まない):

| 変異 | red になった assert |
|---|---|
| (a) `${name}-check.$$.result` → 固定名に戻す | 5 (A が未読を 2 本残す) / 6 (B の登録が A の未読を消さない) |
| (b) notify を `*-check.*.result(N)` の glob に戻す | 2 (他シェルの result を読まない) / 4 (未着なら持ち越す) |
| (c) `add-zsh-hook zshexit _dotfiles_check_cleanup` を削る | 7 (終了時に自分のぶんを掃く) |
| (d) 自己解除の条件を「ディレクトリが空か」に (issue が却下した案) | 3 (他シェルの未読が残っていても外す) |
| (e) `_bg` を `> "$f"` の直接書きに戻す | 8 (親の死後に公開しない) / 9 (公開が原子的) |
| (f) 未着でも無条件に自己解除 | 4 |
| (g) 受け取った result を表示しない | 1 (自分の result を消費して表示) |
| (h) 公開前の `kill -0 "$owner"` を外して無条件公開 | 8 |

- 変異はすべて **共有 working tree の `_zshrc` を書き換えず**、`tmp/` の mutant を
  `ZSHRC_UNDER_TEST` で差し替えて当てた。各変異は diff 行数を目視し、`zsh -n` が通ることと
  テストが**最後まで走ったこと**を確認している (構文エラー / 途中終了は red と混ぜない)
- 途中 2 回、赤の原因が**ハーネス側**だった: ①シェル A を終了させて測ったため A 自身の `zshexit` が
  未読を掃いていた (「B が消した」と区別できない fixture だった) ②`set -u` 下で消し忘れた変数参照が
  ケース 5 以降を丸ごと実行させていなかった (変異 (a) が「4 assert red」に見えていた)。どちらも修正済み

## 残タスク

- **未検証**: ④ の競合そのもの (create と write の隙間で拾う) は時間窓なので直接は再現していない。
  ただし「親の死後に公開しない」側は shim で決定論的に再現・回帰テスト済み。
  テストが固定しているのは「`_bg` は最終パスへ直接書かない (`mv` を失敗させると最終パスが生えない)」
  という構造の側
- **スコープ外**: `_bg` が書かずに死ぬ (shasum 失敗 / ディスク満杯) と配列が空にならず precmd hook が
  シェルの寿命ぶん残る。**現行と同じ挙動なので退行ではない**が、「母集合とカウンタの不一致が
  構造的に起きなくなる」が言えるのは**シェルをまたぐ原因についてだけ**
- **未実測**: SIGKILL 残骸の発生頻度 (上記のとおり窓が狭いことは構造から言えるが、数字は測っていない)
