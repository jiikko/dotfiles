# 022 feat(glogx): `--json` 出力 — ❌ 見送り (2026-07-23)

**結論: 実装しない。** ユーザー判定 (2026-07-23): 人間がシェルから呼ぶことはなく、
「glogx 内部で使うなら実装してよい」の条件付きだったが、glogx は単一バイナリの TUI で
内部に JSON を経由する消費者が存在しないため条件を満たさない。将来、外部スクリプト /
Claude Code が glogx の出力を機械読みする具体的な用途が現れたら再評価する。

## 背景

README「未対応 (必要になったら issue 化)」に挙がっていた項目。スクリプトや Claude Code からの機械利用向けに、コミット + CI 状態 + PR を JSON で吐く静的モード。

**ただし 2026-07-23 の会話で、ユーザーの glogx 利用は ctrl+g popup 経由のみと判明。シェルから直接叩く用途が現状無いため、具体的な消費者 (どのスクリプト / どのワークフローが読むか) が現れるまで着手しない。**着手前にこの前提を再確認すること。

## やること (着手時)

- `glogx --json` で対話ブラウズせず JSON を stdout へ 1 回出力 (`--no-pager` の JSON 版。両立不可の引数組み合わせはエラー)
- スキーマ案:

  ```json
  {
    "commits": [
      {
        "sha": "...", "subject": "...", "author": "...", "date": "...",
        "ci": {"state": "failure", "jobs": [{"name": "lint", "state": "failure", "url": "...", "duration_sec": 13}]},
        "pr": {"number": 123, "state": "OPEN", "url": "..."}
      }
    ],
    "warnings": ["gh が未認証のため..."]
  }
  ```

- CI 取得失敗は `state: "unknown"` + warnings 配列 (終了コード 0 の既存方針を踏襲)
- jobs はコミット一覧の一括取得に含まれる範囲 (name + 集約状態)。詳細 (steps/annotations) は含めない

## 関連

- README の未対応リストからこの issue へ参照を張り替える (着手時)
