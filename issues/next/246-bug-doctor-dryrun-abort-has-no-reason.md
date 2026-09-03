# 246 bug: 下見 (DryRun) の中断が理由の無い「走査し直せませんでした: 」になり、中断したことが画面に出ない

起票日: 2026-09-04
出典: issue 242 の P3-3（下見中も中断できる案内を出す）を直す過程で実測。**使い捨てのテストで実際に再現した**
重要度: **P2**（データは壊れない。ただし「中断した」という自分の操作の結末が画面のどこにも出ない）
対象: `src/doctor/disk/delete.go` の `planDelete`（`fresh.Partial` の分岐）

## 症状

doctor の削除で `d` を押した後の**下見 (DryRun) 中**に Ctrl-C を 2 回押して中断すると、確認画面が

```
      ---  <ラベル>        ❌ できず
           削除の前に走査し直せませんでした:
```

になる。2 つ問題がある:

1. **理由がコロンの後で切れている**（`cur.Reason` が空のまま連結されている）
2. `planHasWork` が false になるので、パネルの見出しは「消せるものがありません」。
   **中断したという事実が画面のどこにも出ない**（「消せるものが無かった」と読める）

## 発火条件（実測 2026-09-04）

`src/doctor/disk` に使い捨てのテストを置いて確認した（確認後に削除した）:

```go
f := newDeleteFixture(t, rmEntry, 64)
r := f.scan(t)
ctx, cancel := context.WithCancel(context.Background())
cancel()
f.opt.DryRun = true
rep, err := Delete(ctx, []Result{r}, f.opt)
// err=<nil> dryrun=true
// entry[0] outcome=failed reason="削除の前に走査し直せませんでした: " items=0
```

## 原因

`delete.go:412`（`planDelete`）:

```go
case cur.Status == StatusBlocked:
    out.Outcome, out.Reason = OutcomeSkipped, "いまは対象外です: "+cur.Reason
    return out
case cur.Status != StatusOK || fresh.Partial:
    return fail("削除の前に走査し直せませんでした: " + cur.Reason)
```

中断で真になるのは **`fresh.Partial` の側**（`Scan` が `ctx.Err() != nil` で立てる。`scan.go:119`）。
このとき `cur.Status` は `ok` のままで `cur.Reason` は空なので、**理由を持たない分岐が理由を連結する
文面を使っている**形になる。

🚨 この分岐は「走査し直せなかった（`cur.Status != StatusOK`）」と「途中で終わった（`fresh.Partial`）」を
**1 つの文言に畳んでいる**。前者は理由を持つが、後者は `status` が `ok` のままで理由を持たない。

`Delete` の DryRun 分岐（`delete.go:229-236`）は `ctx.Err()` を一度も見ない。非 DryRun の
ループは `delete.go:256` で見ており、そこでは `OutcomeSkipped` +
「中断されました (このエントリは触っていません)」と言い分けている。**語彙は既にある**。

## 直し方の候補

`planDelete` の switch で中断を先に見て、非 DryRun 側と同じ語彙へ倒す:

```go
switch {
case ctx.Err() != nil:
    out.Outcome, out.Reason = OutcomeSkipped, "中断されました (このエントリは触っていません)"
    return out
case cur.Status == StatusBlocked:
    ...
```

- `OutcomeSkipped` にすると確認画面の語は `🚫 触れず`（issue 242 の P3-1 で結果画面と揃えた）
- `Failed`（`❌ できず`）のままにするなら、少なくとも理由に「中断されました」を入れる
- あわせて **`Delete` の DryRun ループにも `ctx.Err()` の早期打ち切り**を入れると、
  中断後に残りのエントリを走査し直さずに済む（今は各エントリの `Scan` が個別に落ちるまで回る）

## なぜ今直すか

issue 242 の P3-3 で、実行中パネルの「Ctrl-C を 2 回押すと中断します」を**下見中にも出す**ようにした
（それまで下見中はパネルに抜ける手段が書かれていなかった）。案内どおり押した結果が
「消せるものがありません」+ 尻切れの理由では、案内が嘘に近くなる。

## 関連

- [242](242-research-doctor-ux-audit-2026-09-04.md) P3-3 — この issue の発見元
- [236](done/236-research-doctor-delete-audit-2026-09-04.md) — 削除経路の監査（中断した run の phase を扱っている）
- 231 / 232 が同じ `planDelete` を触っているので、そちらのついでに直せる
