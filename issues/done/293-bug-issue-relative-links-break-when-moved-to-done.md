# issue を `done/` へ移すと本文の相対リンクが切れる（実測 189 件 / 77 ファイル）

- 種別: bug（ドキュメントの参照切れ）
- 対象: `issues/done/**.md`、検査の不在（`tests/issues/` には番号一意と next symlink の検査しかない）
- 優先度: medium

## 症状

issue の本文リンクは `issues/` 直下を起点に書かれているので、`git mv issues/NNN-x.md issues/done/`
した瞬間に**全部 1 段ずれる**。CLAUDE.md は claim symlink の文脈で
「rename すると本文の相対リンクが切れる」と警告しているが（issue 263）、**`done/` への移動には
同じ警告が効いていない**（そちらは日常操作なので、むしろ頻度が高い）。

## 全数（2026-09-06 実測）

```
切れリンク合計: 189 件 / 該当ファイル 77 本
参照先の先頭要素:  _claude 97 / done 57 / next 4 / src 2 / その他 29
issues/done/ 以外にあるもの: 7 件（別原因）
```

数え方（`issues/` 配下の `*.md` から `](…​.md)` を抜き、ファイルの位置から解決する）:

```sh
find issues -name '*.md' -print0 | while IFS= read -r -d '' f; do
  d=$(dirname "$f")
  grep -oE '\]\((\.\./)*[A-Za-z0-9_./-]+\.md\)' "$f" | sed 's/^](//;s/)$//' | while read -r l; do
    [ -e "$d/$l" ] || echo "$f|$l"
  done
done
```

## 原因は 2 パターンだけ（機械的に直せる）

| パターン | 件数 | 例 | 正しい形 |
|---|---|---|---|
| `done/` の中から `done/NNN-x.md` を指す | 57 | `issues/done/194-*.md` → `done/172-*.md` | `172-*.md` |
| `done/` の中から `../_claude/…` を指す | 97 | `issues/done/133-*.md` → `../_claude/rules/…` | `../../_claude/rules/…` |

残り 35 件は個別（移動先が `done/` でない / 参照先自体が消えている等）なので、まとめて直さず 1 件ずつ見る。

## 🚨 group issue の契約変更で射程が広がる（2026-09-06 追記）

issue [291](291-feat-glogx-epic-shows-done-children-by-default.md) で
**group issue の完了・保留先が global `issues/done/` から `issues/epic/<name>/done/`
(と `pending/`) へ変わった**（`1a1a21fa`）。移動の起点も行き先も 1 段深くなるので、
`../` の数がまた変わる:

| ファイルの位置 | `_claude/rules/x.md` を指す正しい形 |
|---|---|
| `issues/NNN.md` | `../_claude/rules/x.md` |
| `issues/done/NNN.md` | `../../_claude/rules/x.md` |
| `issues/epic/<name>/NNN.md` | `../../../_claude/rules/x.md` |
| `issues/epic/<name>/done/NNN.md` | `../../../../_claude/rules/x.md` |

**現時点の影響は 0 件**（実測 2026-09-06: `issues/epic/` は存在せず md も 0 件。
契約変更の前後で切れリンクは 189 件 / 77 ファイルのまま同じ）。つまりこれは
**これから起きる分**で、下の設計に効く:

- **一括修正を `issues/done/` 決め打ちの sed で書かない**。深さは
  「そのファイルの位置から repo root までの段数」で決まるので、**位置から導出**する
  （でないと group issue を直すときにもう一度同じ作業が要る）
- 🚨 **検査を「ディレクトリ名」で絞らない**。`done` / `pending` 決め打ちにすると、
  予約外の綴り（`closed/` `completed/` …）の配下に置かれた md を取りこぼす。291 の実装は
  そういう md を**迷子 `?` として一覧に出す**ように変えた（それまでは黙って消えていた）ので、
  「予約外の綴りにも md は在りうる」が前提になった
- 🚨 **再発防止の検査に `-maxdepth` を使わない**。`issues/epic/<name>/done/` は 4 段目なので、
  深さを決め打ちした検査は**新しい段を黙って対象外**にする
  （[`claude-md-maintenance.md`](../../_claude/rules/claude-md-maintenance.md) の
  「ディレクトリ階層の契約を変えたら深さ前提の検査を grep する」がまさにこの形。
  実例として issue 番号一意の検査が旧深さのまま緑を出し続けたことが記録されている）。
  本 issue の「全数の数え方」に書いた `find issues -name '*.md'` は深さ非依存なのでそのまま使える

## 追測（2026-09-06、291 の実装後）

置き場所別に数え直した。**`done/` だけの問題ではない**:

| 置き場所 | 切れリンク |
|---|---|
| `issues/done/` | 182 件 |
| `issues/pending/` | **7 件** |
| `issues/` 直下 | 1 件 |

`pending/` にも同じ形（`../_claude/…` が 3 件、`done/NNN` が 1 件、兄弟 issue が 3 件）が出ている。
**修正も検査も `done/` 決め打ちにしない**根拠がここにある。

🚨 `issues/` 直下の 1 件は**この issue 自身**だった: 291 を参照していたが、その 291 が
`done/` へ移されて切れた（他セッションの正常な作業）。**この issue を書いている最中に、
この issue が言っている現象を踏んでいる**。頻度の証拠として残す。

## 提案

1. 上の 2 パターンを機械置換で直す（154 件）
2. 残り 35 件を個別に判断（参照先が消えているものは参照ごと消すか、移動先へ張り直す）
3. **再発を止める検査を `tests/issues/` に足す**。既存の `test_next_links_valid.sh` と同じ場所・同じ形
   - 🚨 検査を新設するので [`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md)
     の §0-B（既に答えを出している経路は無いか）と §8（脅威モデルと「検出しない形」を先に書く）を通すこと
   - 「検出しない」を先に決める候補: 外部 URL / アンカーのみ（`#…`）/ コードブロック内の例示パス
   - 🚨 抽出が空だと「違反 0 件 = 緑」になるので、**本走査と同じ関数を通る canary** と
     走査件数の下限を置く（[`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)）

## 対応 (2026-09-06)

提案 1〜3 を全部実施した。

**1. 一括修正 212 件 / 80 ファイル**（md 192 + 非 md 20）。`done/` 決め打ちの sed ではなく
**ファイルの位置から導出**する 3 段: ①`issues/` 起点で書かれたものを今の位置からの相対へ
②repo root 起点で解決するもの ③basename が `issues/` 配下で一意ならそこへ張り直す。
内訳は `../../_claude/rules/` へ 96 / 兄弟 issue へ 89 / `../done/` `../pending/` 6 /
`../../{src,docs,rules}/` 4 / 非 md 20。

**2. 個別 2 件**: 140 の `../_claude/rules/zsh-hook-return-via-reply.md` は移動ではなく**誤り**
（実体は repo root の `rules/`）。issues/ の外からの参照 `src/lockman/README.md` → 091 も直した。
🚨 **直さないもの 2 件**: コードフェンス内 / インラインコード内の**例示**（165 の ```diff、
263 の `](path.md)`）。検査の「検出しない形」に該当する。

**3. 検査 `tests/issues/test_issue_links_valid.sh` を新設**（`make test` の自動発見で走る）。

### 敵対的レビューで出た穴（すべて修正済み）

| | 穴 | 対処 |
|---|---|---|
| P1 | **報告経路が canary の射程外**。canary は抽出と判定までしか通しておらず、「BAD の分類 / 件数の加算 / 最後の `exit 1`」を壊す変異 3 本が全部素通りした | 自分自身を 5 つの fixture（陽性 / 陰性 / 深さ 4 段 / 0 件 / 読めない）へ通す対照を追加 |
| P1 | **フェンスの info string** (```` ```sh title="x" ````) を開始と読まず、閉じの ``` が開始になって**そのファイルの残り全部が無言で落ちる**。リンク総数は健全時と一致するので下限でも見えない | 判定を「行頭 ``` + その後にバッククォートが無い」へ。`~~~` も対応 |
| P2 | ヘッダの「.md 以外は実測 0 件」が**誤り**（20 件現存） | 非 md も修正・検査の射程に入れた |
| P2 | 「深さで絞らない」が宣言だけ（`-maxdepth` の変異が素通り） | **4 段目にだけ**切れリンクを置く対照で固定 |
| P2 | 読めないファイルを「リンク 0 件 = 合格」に畳んでいた | 判定不能として不合格に |
| P2 | 対象 0 件が緑 / 下限が `./issues` と書くだけで無効化 | 0 件は失敗、下限は `pwd -P` の実体比較 |
| P3 | `-not -path next/*` が no-op で、逆に旧運用の実ファイル claim だけ無検査だった | 外した |
| P3 | `~~~` フェンス / アンカー付きリンク (`x.md#sec`) | 対応 |

**変異 9 本すべてで red を確認**（分類の綴り / 加算 +0 / exit 0 / -maxdepth / フェンスを言語名だけへ /
アンカーを落とさない / 非 md を外す / 読めないの報告を消す / 0 件を合格）。

### 残した射程（意図的）

- **内向きの参照は見ない**。issues/ の外から issues/ を指すリンク（`src/**/README.md` 等）は
  検査対象外。今回 1 件だけ実在したので直したが、repo 全体を対象にすると
  「検出しない形」の議論が広がるので、issues/ の中に閉じた
- **4 スペース字下げのコードブロック**は例示として落とさない（markdown のリスト継続行と
  区別できず、落とすと本物のリンクまで無言で消える）。誤検出したら fence か inline code で囲む
- `_claude/agents/tt-api-expert.md` の `](issues/README.md)` は別プロジェクト（ThumbnailThumb）
  向けの agent 定義なので触っていない

## なぜ今直さないか (起票時の記述。上の「対応」で決着)

このセッション（retro [290](290-retro-glogx-audit-x2-and-21-issues-2026-09-06.md) の切り出し）で
**自分が 290 を `done/` へ移した際に同じ壊れ方をして気づいた**もので、その 1 件は直した。
残りは 77 ファイル・依頼と無関係な範囲なので、まとめて直すかは別途判断する。
