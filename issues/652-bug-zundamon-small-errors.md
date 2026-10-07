# 652 (bug): zundamon-kaisetsu の小さな不具合とエラー処理の不揃い

起票日: 2026-10-07

## 詳細

1. **synth が mermaid のキャッシュを「台本から外れた古いファイル」に数える** (`synth.go:cmdSynth`)
   - `os.ReadDir(wd)` の全エントリから keep を除いて数えるので、build が作る `<台本>.work/mermaid/` も数える。そのうえで
     「`<台本>.work/` ごと消して synth し直してもよい」と案内し、従うと mermaid の PNG を描き直すことになる (npx で 1 枚数十秒)
   - 再現 (監査で実施): `s.work/mermaid/` を置いて synth すると「古いファイル 1 件」と出る
   - 直し方: 数えるのを通常ファイルの `*.wav` / `*.query.json` に絞る
2. **中断のときに誤った理由の `error:` が 1 行出る** (`engine.go:startEngine` の `runtimeService` の失敗、`cmdDown` の `stopContainers`)
   - Ctrl-C で runQuietCtx が 130 を返した結果を「サービスが動いていない」「stop が失敗 (rc=130)」として返す。
     他の経路 (image inspect / pull / run) は `interruptedErr()` を先に見ている。終わり方 (同じ signal で死に直す) は正しい
   - 直し方: この 2 か所でも `interruptedErr()` を先に見る
3. **mp4 の出力先が書けないことに、撮影と圧縮を全部終えてから気づく** (`mp4.go:cmdBuild` / `writeMP4`)
   - 最初の確認が `partPath(out)` で、Chrome の撮影と CPU の空き待ち (最大 20 分) の後にある。`-o 無いdir/out --format mp4` で発火 (コードを読んで確認)
   - 直し方: cmdBuild の頭で出力先の dir があり書けるかを見る

## 関連ファイル

- `src/zundamon-kaisetsu/synth.go` / `engine.go` / `mp4.go`。監査の記録: issue 656

## 進捗

- [ ] 1 / [ ] 2 / [ ] 3
