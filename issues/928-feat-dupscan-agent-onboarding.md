# 928 (feat): dupscan のClaude/Codex共通の導入案内・能力診断を用意する

起票日: 2026-10-26

## 目的

CLIを使える環境でもagentが存在・適切なコマンド・対応言語を知らず、普段の探索へ戻る摩擦を減らす。Claude/Codexで共通のCLI契約と短い導入案内を持ち、環境依存の導線は別々に検証する。利用増加と開発効率の改善は未測定。今回は提案のみ。

## 現状の裏取り（5a9a6a98時点）

- `_claude/skills/dupscan/SKILL.md` と `_claude/CLAUDE.md` に使い方がある。
- `_claude/settings.json` は `suggest-dupscan-for-symbol-grep.sh` を呼ぶ。hookはClaude CodeのPreToolUseのBash/Grep入力を読む。grepらしい探索に案内を返し、拒否せず、既定900秒で通知を抑える。cwdのrepoでSwiftを追跡しているかを判定する。
- [921](epic/890/pending/921-feat-dupscan-explore-def-refs-and-grep-nudge.md) の案内hookは実装・push済みで、残りは1か月後の効果測定。新規のhookが必要だと一括して扱わない。
- `main.go` のusageは人向けで、対応コマンド・問い合わせ形式・言語別対応をmachine-readableに返す専用コマンドはない。Codexの各実行環境でこのClaude用hookが使えるかは未検証で、共通に動く前提にしない。

## コマンド・導線案（未実装）

```sh
dupscan capabilities -format json
dupscan doctor
dupscan agent-guide -target claude -print
dupscan agent-guide -target codex -print
```

### 共通のCLI契約

- capabilitiesは実装済みの言語×コマンド、問い合わせ形式、schema/版、近似・制約を短く返す。900でGoが加わっても、Goのdef/refs/callersまで使えると誤案内しない。実際のdispatchと同じ情報源を使い、手書きの対応表を別に増やさない。
- doctorはPATH、repoの認識、検査対象の言語、設定の読取、gateのbase候補等を診断する。設定ファイルが無くても動く現行契約を保ち、「`.dupscan.json` がない」だけで使えない扱いにしない。
- agent-guideは「既存helper探し→index/search」「定義→def」「参照→refs/callers」「実装後→gate」の短い例とfallbackを、その版が対応する範囲で出す。新機能の925〜927が無い版でも、使える入口を案内する。
- 既定では案内/差分を表示する。AGENTS.md / CLAUDE.md / skillや設定を書き換える導入処理を設ける場合は明示opt-inとし、既存指示との重複・競合を扱う。

### Agent別の接続

- Claudeは既存921のhookを活用し、無差別に別hookを重ねない。Codexはまず通常のshell経路とrepository instructionsでCLIを発見・実行できるか検証する。hook等の固有機構を使うなら、その環境の仕様と実動作を確認して別のadapterにする。
- セッション開始やtool呼び出しのたびに全repoを走査しない。初回buildも通常の対話的な導入経路で扱い、軽い通知hookから自動buildを起こさない。wrapperが初回buildする現行仕様を踏まえ、未buildを軽く診断する方法を設計する。
- baseは信頼するCLI引数/upstream等から決める現行の契約を保ち、生成した設定で `base=HEAD` を固定してgateを弱めない。
- 通知の回数・長さ・失敗時の挙動を設け、未対応言語やSDKの検索、文字列/設定の検索を誤って誘導しない。repoのソース/docは解析対象データとして扱い、命令やshell文字列として自動実行しない。

## 検証・受け入れ条件

- [ ] Claude/Codexの各利用環境について、発見経路・利用できたコマンド・未対応の機構を記録し、動作確認できた組だけ案内する。
- [ ] CLI不存在/未build/設定なし/壊れた設定/別cwd・worktree/対応言語が混在/非git repo/SDKのシンボルを含むケースを検査する。
- [ ] capabilities・guide・実際のCLIの対応表が一致し、初回buildと探索そのものの時間を分けて測る。導入の繰り返しで指示を重複させない。
- [ ] hookの通知は少数・短文で、追加の待ち時間を実測する。全repo解析やbuildが通知だけで起きないことを確認する。
- [ ] [917](epic/890/pending/917-test-dupscan-usefulness-verification.md) / 921の計測とつなぎ、両agentを別集計する。適切な呼び出し、実際の再利用、誤誘導、往復数・elapsed timeを比較し、利用回数の増加だけで採用しない。
- [ ] Codex/Claudeの固有連携が使えなくても、共通CLIを手動で呼べる導入経路が残る。

## 進捗

- 2026-10-26: 起票。既存Claude hookと新たな共通入口を分けた。CLI・設定・agent連携の実装は未着手。
- 2026-10-26: Codexの読み取り専用subagentで、CLIの事実・互換性とagent導線・効果測定の2観点から反証レビュー。P1/P2/P3の指摘なし。静的レビューであり、新機能の実装・性能・agentによる利用効果を検証したものではない。
- 2026-10-26: 相対リンク・diffと、Bash 5でのissue関連テストを検査して成功。提案の本文を記録し、機能の実装・効果測定はopenの作業として残す。
