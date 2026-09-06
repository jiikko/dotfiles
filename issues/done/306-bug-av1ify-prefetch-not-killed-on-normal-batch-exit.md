# bug: av1ify のバッチが正常終了で prefetch を kill せず、次回入口で追跡不能になる

起票日: 2026-09-06
カテゴリ: bug
優先度: 低（prefetch は `head -c 1` なのでローカルファイルでは即終了する。効くのはクラウドの
materialize が長引くケース。実発は未確認 = コード上の経路だけ確定）
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。2 エージェントが独立に検出

## 何が起きているか

`zshlib/_av1ify.zsh:__av1ify_run_batch` は `__av1ify_kill_prefetches` を
**中断パス（`return 130`）でしか呼ばない**。

```zsh
__av1ify_run_batch() {
  ...
  __AV1IFY_PREFETCH_PIDS=()          # ← 前回の PID を kill せずに捨てている
  for (( i = 1; i <= n; i++ )); do
    ...
    if ...; then ((ok++))
    else
      if (( exit_status == 130 || __AV1IFY_ABORT_REQUESTED )); then
        __av1ify_kill_prefetches; return 130     # ← ここだけ
      fi
      ...
    fi
  done
  ...
  return 1     # NG あり。kill なし
  ...
  return 0     # 全件 OK。kill なし
}
```

冒頭の `__AV1IFY_PREFETCH_PIDS=()`（:622）は **kill する前にクリアしている**ので、
前回のバッチの prefetch は**二度と殺せない**。PID 再利用の誤 kill を避ける工夫
（`__av1ify_prefetch` の `kill -0` による間引き）の代償が、ここで「追跡不能なプロセス」に化けている。

## 推奨対応

1. **`__av1ify_run_batch` の全出口**（正常 / NG / 中断）で `__av1ify_kill_prefetches` を呼ぶ。
   `zshlib/_concat.zsh:537` が同じ理由で `always` ブロックを使っている（先例）
2. :622 のクリアは、**クリア前に kill する**か、既に :63-70 にある `kill -0` の生存確認を
   クリア時にも通す

機能は削らない: prefetch は常に `targets[i+1]` へ撃たれるので、ループ終了時に生き残っているのは
**処理済みファイルの materialize だけ**。

## 検証

長時間走る fake を prefetch させ、正常終了後に `kill -0` が偽になることを**上限つきポーリング**で見る
（壁時計に依存させない。[`avoid-wall-clock-assertions.md`](../../_claude/rules/avoid-wall-clock-assertions.md)）。
**変異検証**: 正常終了パスの kill を消して red を確認する。

## 監査側の根拠のうち 1 件は誤り（記録）

一次報告の「`tests/zshrc/av1ify/test_av1ify_prefetch.sh` の Test 1-6 は `kill_prefetches` 単体しか
見ていない」は**不正確**。実際は Test マーカーが 37 個あり、Test 7-9 は `__av1ify_run_batch` /
`-f` モードの統合（次ファイルの prefetch 順序）まで見ている。
**核心（正常経路の kill を pin したテストが 0 件）は正しい**ので指摘自体は採るが、
数の主張は機械で数えてから書くこと。

## 対応済み（2026-09-07 / commit ef1b5c70）

`zshlib/_av1ify.zsh` の `__av1ify_run_batch` を薄いラッパーにし、実体を
`__av1ify_run_batch_impl` へ移して、**正常復帰でも** `__av1ify_kill_prefetches` を通すようにした。

```zsh
__av1ify_run_batch() {
  __av1ify_run_batch_impl "$@"
  local _rc=$?
  __av1ify_kill_prefetches
  return $_rc
}
```

### zsh の `always` を採らなかった理由

最初は `{ ... } always { ... }` で書いたが、**`make test` の shellcheck が SC1072 で落ちる**
（`_av1ify.zsh` は `Makefile:7` の shellcheck 対象側に意図的に置かれている）。
`ZSH_SYNTAX_FILES` へ移す案は、ユーザー入力のファイル名に対する SC2086 の検査を失うため却下した。
ラッパー化なら bash 構文の範囲に収まる。

### 変異検証

- ラッパーの `__av1ify_kill_prefetches` 呼び出しを `: # MUTANT` に置換 → **red**（Test 18）
- 最初に当てた「`always` ブロックごと削除」は**波括弧が閉じずパースエラー**になったので、
  red でも green でもない第 3 の結果として扱い、ビルドできる形で当て直した

### 残した設計判断

バッチ開始時の `__AV1IFY_PREFETCH_PIDS=()` は**そのまま**にした。ここで kill すると、
`__av1ify_kill_prefetches` に生存チェックが無いぶん **pid 再利用で無関係のプロセスを撃つ**
可能性が出る。開始時のリセットは「前バッチの pid を持ち越さない」目的で足りている。
