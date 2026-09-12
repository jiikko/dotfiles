# 369 research: codex-drive の実行時間と Claude 側の作業負荷を、出力の質を落とさずに下げる

- 起票: 2026-09-12 / 構成の見直し: 2026-09-13 (6 案の並列から「案 0 → 計測 → 3 段」の順序つきへ)
- 種別: `research` (skill の運用改善。結論が出た項目から `~/.claude/skills/codex-drive/SKILL.md` へ反映する)
- 出典: obaket の 3 epic (quicksearch-headless 747 / bandwidth-limit 650 / deterministic-test-time 736) を
  codex-drive で並行して回した 2026-09-03〜09-12 の実測と retro (obaket 725 / 763 / 765)
- 前提: codex-drive は今後も継続して大きく使う (ユーザー確認 2026-09-13)。1 マイルストーンあたりの往復は毎回払うコスト
- **対象外**: usage limit による停止 (避けようがないので考慮しない。ユーザー指示 2026-09-12)

## 観測 (obaket、2026-08-30 → 09-12 の 13 日間)

| epic | commits | production | tests | issue/docs | 状態 |
|---|---|---|---|---|---|
| 747 quicksearch | 63 (4 日) | +4.4k | +7.3k | +2.7k | 完了 |
| 650 bandwidth | 70 (10 日) | +9.5k | +20.6k (+golden 8.4k) | +2.9k | 未リリース (gate false) |
| 736 deterministic | 100 (6 日) | +5.2k | +6.3k (+gate 2.6k) | +3.5k | 段階 4 作業中 |

- テスト行数が 108.7k → 162.2k (**+49%**)、production は +18%。`macOS/bin` に 13 日で 33 ファイル追加
- 650 の issue 本文は 1,917 行 (checkpoint を issue に積む運用)
- 時間を食っている箇所は retro に実測がある:
  - codex は macOS target の型検査ができず、**マイルストーンごとに 2〜3 往復** (763 項目 4。SwiftLint 違反 /
    `?? Never` / MainActor 隔離 / 捏造 API)
  - 敵対レビューが **2 ラウンド連続で 8〜9 件** (725)。ただし修正差分が新しいバグを作り 2 周目が拾った実例が
    2 件 (763 項目 1 / 765 項目 2) あるので、周回は削れない
  - codex の変異検証は sandbox で `swift test` が走らず typecheck だけで「成功」と書く (650 M1)。
    **Claude が全部当て直す**ので、codex に回させた分は丸ごと無駄
  - Claude 自身の全数勘定の誤り 2 件 / codex の変異結論の誤り 2 件 (734 M3)。D1.5 のクロス批評が拾ったが各 1 ラウンド
  - 敵対レビューの指摘に対し codex が **production に test 専用 seam** を足してくる (650 M1 fix3) → 差し戻しで 1 往復

🚨 **どの工程に壁時計が何分かかったかは記録が無い**。`codex-fanout` の `runs.tsv` は label / rc / out / log の
4 列で、所要時間を持たない (2026-09-13 に `bin/codex-fanout` の台帳生成部で再確認)。最適化の前にここを測る
(`perf-claims-need-measurement.md`)。

## 原因の見立て (2026-09-13 追記)

上の時間食いのうち **型エラー往復 / 走らない変異ループ / sandbox 由来の偽赤 / 「xcodebuild を試みさせない」の縛り**は、
すべて **codex の sandbox (`-s workspace-write`) が xcodebuild / SwiftPM / clang のキャッシュ書き込みを拒む** 1 点から
派生している (SKILL.md の Error 74 の注記群が全部この派生)。旧版の案 2〜3 はその症状への対処で、原因を外す案が無かった。
不具合対応の原則 (パッチワークより前提の是正) に従い、**原因側を案 0 として最上流に置く**。

## 順序

```
案 0 (sandbox の制限を外す) ─┐
                             ├─ 案 1 (計測) を先に入れ、案 0 の効果を 1 マイルストーンで測る
段階 1 (計測不要の chore) ───┘
段階 2 (内訳を見てから決める)
段階 3 (skill から外して repo 側へ)
```

## 案 0: sandbox の制限を外す (原因の除去。旧版に無かった案)

**決定 2026-09-13: 案 0-b (`-s danger-full-access`) を採用** (ユーザー承認)。SKILL.md 4.3.0 に反映済み
(実装 / 競作 / `[3.7]` の雛形を `danger-full-access` に、旧 Error 74 規律は「外せない環境向け」として残置、
セットで守る 4 項目 = DerivedData 分離 / 作業根の外へ書かない + 検閲 / git 禁止の検閲格上げ / 破壊的操作の log 読み)。
案 0-a は**外せない環境向けの代替案**として残す (未実施)。

codex-cli 0.154.0 で確認したオプション (2026-09-13、`codex exec --help`):

- `-s, --sandbox <read-only | workspace-write | danger-full-access>`
- `--dangerously-bypass-approvals-and-sandbox` (approval も消えるので**使わない**)

検討した 2 段 (a は不採用・代替案として残置、b を採用):

- **a. sandbox は残し、キャッシュ先だけ書き込み許可に足す** — `-s workspace-write` のまま、DerivedData /
  SwiftPM cache / clang module cache を writable root に加える。キー名は公式 config reference で確認済み
  (2026-09-13。`[sandbox_workspace_write]` の `writable_roots: array<string>` = "Additional writable roots when
  sandbox_mode = workspace-write")。CLI からは `-c` で渡す:

  ```sh
  codex exec -s workspace-write \
    -c 'sandbox_workspace_write.writable_roots=["/Users/koji/Library/Developer/Xcode/DerivedData","/Users/koji/Library/Caches/org.swift.swiftpm","/Users/koji/Library/Caches/clang"]' \
    ...
  ```

  🚨 **`~` 展開は reference に記載が無い**ので絶対パスで書く。同じ table に `network_access` /
  `exclude_tmpdir_env_var` / `exclude_slash_tmp` もある (既定で `$TMPDIR` と `/tmp` は書ける)。
  xcodebuild は `/var/folders` やツールチェイン側にも書くので、**それで通るかは 1 マイルストーンの実測で確定する**
  (通らない書き込み先を Error 74 のログから列挙して足す)。VS Code 拡張が `writable_roots` を無視する既知 issue
  (openai/codex #8029) があるが、CLI の `codex exec` は対象外
- **b. `-s danger-full-access` で sandbox を切る** — 🚨 **codex が worktree の外 (他 checkout・ホーム) へ書けるようになる**。
  skill は git 操作禁止をプロンプトで縛っているだけで、sandbox はそれを強制していない (SKILL.md 「workspace-write は
  prompt の禁止を強制しない」)。爆発半径が「同じマシンの他 checkout とホーム」まで広がることを**ユーザーが受け入れた**
  (2026-09-13)。守りは sandbox からプロンプト + Claude の検閲へ移る (本体 checkout の `git status` 不変 / worktree の
  `git log -1` 不変 / 要約と log に破壊的操作が無いこと)

どちらの段階でも**セットで要る指示**:

- **worktree ごとに `-derivedDataPath` を分ける**。codex が worktree で xcodebuild を回し、同時に Claude が本体で
  xcodebuild / `swift build` を回すと DerivedData と package graph が共有されて無限待ちになる
  (`no-concurrent-spm-build-during-xcodebuild.md` がそのまま当たる。obaket で 7.5 時間停止の実例)
- 効いたら SKILL.md の Error 74 系の注記 (「xcodebuild を試みさせない」「shared の swift build まで」「偽赤の再実行」) を
  **条件つき** (sandbox を外せない repo 向け) に書き換える。全部消さない: 外せない repo は残る

質を落とさない根拠: codex が自分で型検査・test を回せるようになるだけで、Claude の検閲 (`[3]` / `[3.8]` の当て直し) は
変えない。減るのは「codex が実行できなかったものを Claude が代行する」往復。

## 案 1: 計測を先に入れる (前提。案 0 の効果を言うための基準)

- `codex-fanout` の `runs.tsv` に **開始時刻 / 所要秒** を足し、merger の行も台帳に載せる (今は merger は別起動で行が無い)。
  `codex-run` は fanout に委譲しているので同じ列が出る
- **これだけでは壁時計の内訳は測れない** (codex run 単位しか取れない。merger / Claude の digest 読み / build・test / 修正 /
  issue 更新は外)。マイルストーン単位で「開始・終了時刻 / codex 往復回数 / 型エラーによる差し戻し回数 / 敵対ラウンド数 /
  読んだ digest 行数」を checkpoint に 1 行で残し、run 合計と通しの壁時計を**別々に**書く (`perf-claims-need-measurement.md`)
- 質を落とさない根拠: 記録するだけ
- 案 0 とは独立に価値がある: rc しか無い台帳では「静かに遅くなった run」に気づけない

## 段階 1: 計測を待たずに入れる chore (質の判断を含まない)

### 1-1. 走らない repo では `[3.8]` の実行ループを codex に回させない (旧案 3)

- skill は既に「変異検証は codex の報告を数に入れず、Claude が `[3.8]` で必ず自分で当て直す」と書いている (SKILL.md 検証節)
  のに、走らないと分かっている repo でも codex の実行ループを毎回回している。**skill の中で矛盾している**
- **その repo で走らないことが 1 度分かったら以降は最初から Claude のハーネスで当てる** (変異 patch の生成だけ codex read-only に
  作らせる)。obaket macOS は案 0 が効くまでこの条件に当たる
- 減るのは走らない実行ループ 1 回 (max effort・15〜40 分)。Claude 側の作業は増える (明示的な例外として skill に書く)
- 案 0 が効いた repo では不要になる (条件つきの記述にする)

### 1-2. codex が返した直後に型検査、通してからレビュー fanout (旧案 2 の順序部分)

- 現行 skill は「型 error は Claude の `make test` で 1 往復として織り込む」と既に書いている。新しいのは**順序だけ**:
  `[3]` の型検査 (`make build` 相当) を **レビュー fanout を起動する前に**回す。今はレビューと並走させて型エラーを後から知る形になりがち
- shared SPM を触るマイルストーンでは、実装プロンプトに「返す前に `cd shared && swift build --build-tests` を通す」を固定文で入れる
  (sandbox で走るかは repo ごとに未確認 = 最初のマイルストーンで確かめてから固定する)
- 質を落とさない根拠: 型エラーはどの往復でも最終的に直る。減るのは「型エラーを抱えたままレビューに出す」往復
- 却下した形: 「macOS の配線は Claude が書く」— skill の役割分担 (Claude は重い実装を書かない) と衝突する
- 案 0 が効けば codex 側で型が通るようになり、この項は「Claude 側の順序」だけが残る

### 1-3. luna max の出力の質が低いときは astra 系モデルへ切り替えてよい (ユーザー指示 2026-09-13)

- 現行 skill は `gpt-5.6-luna` + `model_reasoning_effort="max"` 固定 (541 の実測で low は不可、と却下節にある)
- **luna max で「動くが筋が通っていない」「捏造 API」「同じ型エラーを 2 往復しても直らない」等、出力の質が低いと Claude が
  判定したマイルストーンでは、`gpt-6-astra` へ切り替えてよい** (`codex exec -m gpt-6-astra -c model_reasoning_effort="max"`)
- モデル ID の出典 (2026-09-13 調査): OpenAI は 2026-09-03 に GPT-6 Astra を公開。ID は `gpt-6-astra`、codex-cli は
  **0.153.1 以上**が要件 (手元は 0.154.0 で満たす)。reasoning effort は `low / medium / high / xhigh / max` の 5 段。
  GPT-5.6 系は `gpt-5.6-sol` / `gpt-5.6-terra` / `gpt-5.6-luna` の 3 変種 (openai/codex #43398 に ID が列挙されている)
- 🚨 **段階的 rollout (Trusted Access) なので、アカウントで使えるかは未確認**。`codex models` は TUI で非対話では出ない
  (実測: `stdin is not a terminal`)。反映前に `codex exec -s read-only -m gpt-6-astra` の 1 行 probe で実在を確かめる
- 🚨 capacity 死は luna と共通 (#43398: 2026-09-07 に astra / sol / terra / luna / 5.5 が同時に "at capacity"、mini だけ生存)。
  切り替えは capacity の回避策にはならない (対象外の usage limit と同じ扱い)
- 切り替えは**マイルストーン単位**で、checkpoint に「M<n> は astra へ切り替え。理由: …」を 1 行残す (前後比較の材料)
- 質を落とさない根拠: 切り替えの判定は Claude の検閲結果 (`[3]` で弾いた回数) に基づく。上げる方向の切り替えなので
  541 の「low で質が落ちる」とは向きが逆

## 段階 2: 1 マイルストーン測ってから決めるもの (判断基準の変更を含む)

### 2-1. 敵対レビューの「小修正」を Claude が直接当てる範囲を広げる (旧案 4)

- 現行例外は「確定的な 1〜2 行」。**「1 ファイル・20 行以内・設計判断を含まない (契約 / 不変条件 / 責務を変えない)」まで広げる**新規判断。
  行数は目安で、判定の軸は「設計判断の有無」。迷ったら codex に戻す
- skill の役割分担 (Claude は重い実装を書かない) を少し崩す方向なので、**内訳で「修正指示の作文と往復」が太いと分かってから**触る
- 「r2 は r1 で新設したものだけを攻める」は `adversarial-review-own-safeguards.md` §7 に既にある (新案ではない。運用で守る)
- 質を落とさない根拠: 直した差分は §7 どおり r2 が攻める

### 2-2. checkpoint の「経緯」の量に上限を置く (旧案 5。必須項目は減らさない)

- 650 は 1,917 行。issue/docs が 3 epic で +9.1k 行 (全差分の約 12%)。Claude のトークンで書いている
- skill が checkpoint に必須とするもの (採用設計の要点 / M 表と commit hash / 手順 / 再開方法) と、ルールが要求する
  「全数勘定・却下した指摘と理由」は**減らさない**。上限を置くのは **「踏んだ罠」「経緯」「敵対レビューの原文引用」**の節で、
  各 M で 10 行以内にし、原文は tracked な `macOS/docs/` の設計 doc か digest の**要約**に置く (`tmp/` は消えるので参照先にしない)
- 内訳で「issue 更新」が太いと分かってから触る
- 質を落とさない根拠: 再開に要る情報と却下理由は全部残る。落ちるのは経緯の再説明

## 段階 3: skill から外して repo 側へ送るもの

### 3-1. codex が書くテストの規約 (旧案 6。obaket issue 781 の実例)

- 同じ repo で 3 epic を並行させた結果、一方の epic が禁止した形 (実時間待ちの `pollUntil`) を他方の codex が量産した
  (TransferUploadBodyTests 0→53、QuickSearchSessionTests 0→28。gate の射程外)
- これは **obaket の `[R]` 要件テンプレに書く内容**で、skill の汎用文にすると他 repo で意味を持たない固定文が増える。
  skill 側には「repo 固有のテスト規約は `[R]` テンプレの prompt 部品として毎回連結する (fanout は共通 header を自動注入しない)」
  の 1 行だけ置き、文言 (「unit test では時間・スケジューリングを注入し、`pollUntil` / `Task.sleep` を新規に書かない。
  実時間そのものを検証する integration test は別に分けて明示する」) は obaket 側へ

## 却下した案 (理由を残す。次に同じ案が再生成されないため)

- **`[3.5]` と `[3.6]` を 1 回の fanout に畳む**: `[3.6]` は `[3.5]` の修正後の green を前提にし、同時実行は skill が禁止している。
  lens を 1 本に減らす形は「視点の多様性」に反する。修正後に `[3]` を飛ばす形は検閲省略。**採らない**
- **production への test seam 禁止をプロンプトに足す**: 実装プロンプト・`[R]`・`[3]` の 3 箇所に既にある。新案ではない
- **effort / モデルを下げる**: 541 の実測 (low で「動くが筋が通っていない」実装) があり、ユーザー決定で max 固定。
  上げる方向 (1-3 の astra) は別
- **敵対レビューのラウンド数を固定で 1 にする**: 763 / 765 で 2 周目が実害を拾っている
- **lens 本数を減らす**: 「独立した視点」が質の源。減らすのは起動回数であって観点ではない
- **`--dangerously-bypass-approvals-and-sandbox`**: sandbox だけでなく approval も消える。案 0 は `-s danger-full-access` で足りる
- **「macOS の配線は Claude が書く」**: skill の役割分担と衝突 (1-2 に記載)

## 受け入れ条件

- [x] 案 1 (計測) を `bin/codex-fanout` に入れた (2026-09-13): `runs.tsv` を label / rc / **started_at / elapsed_s** / out / log の
      6 列にし、merger を回したときは末尾に merger 行 (成否に関係なく) を追記。label `merger` は予約語として起動前に弾く
      (merger.rc 等との衝突は元から在った潜在バグ)。bats に 3 つの assert (ヘッダ完全一致 / timeout 2 秒の run の
      elapsed ≥ 2 / -M では merger 行なし) と予約語テストを追加。commit: 「feat(codex-fanout): runs.tsv に所要時間と merger 行」
- [x] 段階 1 の 1-1 / 1-2 を SKILL.md 4.4.0 へ反映 (1-1: `[3.8]` 節の例外 + 外せない環境の段落 / 1-2: `[3]` 冒頭の順序の項 +
      `[3.5]` の「green の前に起動しない」+ shared の固定文)。commit: 「docs(codex-drive): 1-1 / 1-2 を反映」
- [x] 1-3 (astra への切り替え): probe 実測 2026-09-13 `codex exec -s read-only -m gpt-6-astra` (effort low) → rc 0、
      応答 "GPT-6"、3,800 tokens。アカウントで使える。SKILL.md 4.5.0 の「モデルは振らない」の例外項として反映、
      effort 整合テストに FALLBACK_MODEL の allowlist を追加 (SKILL.md から消えたら検査も落ちる形)
- [x] 案 0-b をユーザーが承認 → SKILL.md 4.3.0 へ反映 (雛形 3 箇所 / Error 74 規律の条件化 / セットの 4 項目)。
      commit: 「feat(codex-drive): 実装 run の sandbox を既定で外す」
- [ ] 案 0-b を obaket の次マイルストーンで実測し、checkpoint に残す: Error 74 の有無 / 型エラー往復数 (旧 2〜3) /
      codex が xcodebuild を自分で回せたか / 本体 checkout と worktree の git state がはみ出していないか / 要約・log に
      依頼外の破壊的操作が無いか。**危険側の観測 (はみ出し・破壊的操作) が 1 件でも出たら 0-a へ戻す**
- [ ] 案 0-a は 0-b で危険側の観測が出た場合の代替として残す (未実施)
- [ ] 案 1 の内訳を 1 マイルストーン分取り (run 合計と通しの壁時計を別々に)、段階 2 のうち太い工程に当たるものだけ反映する
- [ ] 段階 3 は obaket 側の `[R]` テンプレへ移し、skill には 1 行だけ残す (**このマシンに obaket の checkout が無い**
      (2026-09-13 `mdfind` / `~/src` 走査で 0 件) ので、obaket を持つマシンのセッションで行う)
- [ ] 質の比較は件数の増減で判定しない。**同じ変異セット**での red / green / hang の結果表と、r1 の P1 の**内容**を前後で並べ、
      「前は拾えていた種類の指摘が消えていない」ことを Claude が読んで確認する

## 関連

- `~/.claude/skills/codex-drive/SKILL.md` — 対象
- `bin/codex-fanout` — 案 1 の対象 (台帳生成部)
- obaket `issues/done/725-retro-722-724-codex-drive-2026-09-06.md` / `763-retro-quicksearch-headless-step2-2026-09-09.md` /
  `765-retro-quicksearch-headless-step3-2026-09-10.md` — 実測の出典
- obaket issue 781 — 3-1 の具体例 (実時間待ちテストの量産)
- `_claude/rules/perf-claims-need-measurement.md` — 案 1 を先にやる根拠
- `_claude/rules/no-concurrent-spm-build-during-xcodebuild.md` — 案 0 で codex に xcodebuild を回させるときの並行禁止
- codex 反証レビュー (2026-09-12、read-only 1 本、旧版に対して): P1 4 件 (旧案 5 の同時実行と lens 削減 / 旧案 2 の xcodebuild と
  役割分担 / 旧案 7 の再開情報の欠落 / 案 1 の測定範囲) と P2 5 件をすべて反映した。obaket 側の実測値は codex の参照範囲外で未反証
- 案 0 / 1-3 の出典 (2026-09-13 web 調査): [Codex config reference](https://learn.chatgpt.com/docs/config-file/config-reference)
  (`sandbox_workspace_write.writable_roots` と `-c` 構文) / [Codex sandboxing](https://developers.openai.com/codex/concepts/sandboxing) /
  [GPT-6 Astra の codex 設定記事](https://codex.danielvaughan.com/2026/09/03/gpt-6-astra-codex-cli-configuration-context-notes-safety/)
  (ID・最低版・effort 5 段) / [openai/codex #43398](https://github.com/openai/codex/issues/43398) (モデル ID 一覧と capacity 死) /
  [openai/codex #8029](https://github.com/openai/codex/issues/8029) (VS Code 拡張が writable_roots を無視する既知 issue)
- 🚨 2026-09-13 の追記 (案 0 / 1-3 / 段構成) は**外部反証を通していない**。案 0 のオプション実在は `codex exec --help` で
  実測、キー名と ID は上の一次資料で確認、効くかどうか・使えるかどうかは受け入れ条件の実測で確かめる
  (反証されていない仕様として扱う)
