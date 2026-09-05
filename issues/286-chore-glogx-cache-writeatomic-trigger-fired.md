# chore: `~/.cache/glog` への書き込み規約 — issue 219 が残した trigger が発火している

起票日: 2026-09-06
カテゴリ: chore
優先度: 低（実害は「画面状態が復元されない」まで。本題は規約のドリフト）

## 何が起きているか

`cacheBaseDir()` 配下へ書く経路のうち **3 本が素の `os.WriteFile`**:

| 経路 | 導入 |
|---|---|
| `issues_state.go:saveIssuesScreen` | 2026-07-31（`2df4efa8`） |
| `doctor_resume.go:saveDoctorScreen` | **2026-09-04**（`15ab169d`） |
| `ratelimit_resume.go:saveRatelimitScreen` | **2026-09-05**（`303d4163`） |

残り 4 本（`cache.go` / `claude_version.go` / `usage_cache.go` / `doctor_cache.go` ×2）は
`writeAtomic` を通る。

`issues/done/219` は「`~/.cache/glog` へ書く経路は 4 本」と数え、doctor の 2 本を
`writeAtomic` へ寄せた。その修正は **2026-09-03**。
つまり**後の 2 本は 219 の 1〜2 日後に、規約の外側で生えている**。

## 🚨 これは「ゲートを忘れた」ではない。219 は意図的に見送り、trigger を書いていた

`issues/done/219` の「入れなかったもの」節:

> - **ruleguard / depguard による強制**: 第 1 段では入れない。…ruleguard も構文しか見ないので
>   「どの置き場への書き込みか」は表現できず、package 全体の `os.WriteFile` 禁止 + 例外リストになる。
>   **trigger: 3 本目の複製が出たとき、または `issues_state.go:71` を直す判断が出たとき**

219 は `issues_state.go` の存在も**知っていて**、射程に入ることまで書いている。

したがって本 issue は「規約が破られた」ではなく、**「219 が置いた trigger の再評価が必要になった」**。

### trigger の厳密な読み

- 「3 本目の**複製**」= `writeAtomic` を手で書き直した実装。`doctor_resume.go` /
  `ratelimit_resume.go` は素の `os.WriteFile` なので**複製ではない** → この節は厳密には未発火
- 「`issues_state.go:71` を直す判断が出たとき」→ **本 issue がその判断を求めている**

## 失敗モードは 219 とは違う（重みの正直な見積もり）

219 が問題にしたのは「固定名 tmp + `os.WriteFile` の複製実装が掃除分岐を落とす」形で、
**残骸が残る**のが症状だった。

素の `os.WriteFile` は `O_TRUNC` なので**残骸ではなく途中書きの JSON が残る**。
次回起動で parse に失敗して既定へ落ちる（各 loader が壊れた値を「復元しない」へ倒す設計）。
実害は**画面が復元されないだけ**。

## 監査中に出た反対意見（記録）

3 体のうち 1 体は本件を**却下**していた:

> 画面状態 3 本はいずれも素の `os.WriteFile` で揃っており、これは「壊れたら復元しないだけ」の
> 安全に縮退する状態なので**意図的な規約**と読める。219 が問題にしたのは
> 「temp+rename を手で書いて掃除分岐を落とした」形で、素の WriteFile とは別

失敗モードの区別は**正しい**（上節に取り込んだ）。ただし「意図的な規約」という読みは
219 が `issues_state.go:71` を射程に挙げている事実と食い違うので、却下ではなく
**trigger の再評価**として起票した。

## 推奨対応（どちらかを選ぶ判断が要る）

**A. 規約に寄せる**: 3 本を `writeAtomic` へ寄せ、ソース走査テストで固定する。
検出の形: `src/glogx/*.go`（非テスト）を走査し、`cacheBaseDir()` 由来のパス変数を
`os.WriteFile` へ渡す呼び出しを違反にする。既存の `waitdelay_discipline_test.go` /
`clock_rollback_test.go` と同じ枠なので新しい仕組みは要らない。
🚨 **抽出が 0 件でも緑にならないよう、既知の `writeAtomic` 呼び出し件数に下限 canary を置くこと**。

**B. 例外として明文化する**: 「画面状態の保存は復元失敗に安全に縮退するので atomic 不要」を
3 ファイルの直近コメントに書き（`pending-issue-rationale-in-code.md`）、219 の done 本文にも
1 行追記して trigger を閉じる。

**どちらでもよいが、現状（規約が在るが 3 本が外れていて理由がどこにも無い）は避けること。**
次の監査が同じ指摘を再生成する。

## 反証の試み

3 ファイルとそれぞれの `*_test.go`、`cache.go` の `writeAtomic` 周辺コメント、
`.golangci.yml` / `gorules/rules.go` / `tests/` を探したが、
「ここは atomic でなくてよい」旨の記述は **0 件**。

## 関連

- `issues/done/219`（規約の出典と trigger）
