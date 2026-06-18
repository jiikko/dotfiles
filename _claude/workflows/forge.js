export const meta = {
  // 名前は 'forge-fanout' (skill 'forge' との一覧上の衝突を避ける)。
  // 起動は scriptPath 経由なので name 解決には依存しない。
  name: 'forge-fanout',
  description: 'Forge の fan-out オーケストレーション本体: 専門家エージェントの並行調査/レビュー/反復デバッグ + クロスレビュー + 統合を決定論的に実行する',
  whenToUse: 'forge skill (SKILL.md) から scriptPath で起動される。人間ループ (モード選択/設計承認/修正方針) は skill 側、重い fan-out はこのファイル側。',
  phases: [
    { title: 'Investigate' },
    { title: 'Cross-Review' },
    { title: 'Integrate' },
    { title: 'Review' },
    { title: 'Ultra Rounds' },
    { title: 'Ultra Integrate' },
  ],
}

// ───────────────────────────────────────────────────────────────────────────
// forge.js — Forge skill の fan-out 実行エンジン
//
// 役割分担 (SKILL.md と対応):
//   skill 側 (main Claude, 対話あり): Phase -1 モード選択 / Phase 0 要件確認 /
//     Phase 1.5 設計承認 / Phase 2 実装 / Phase 3 セルフレビュー / Phase 5 修正方針 /
//     Phase 5.3 codex-review / 完了レポート
//   この workflow 側 (決定論的 fan-out): Phase 1+1.1 (investigate) /
//     Phase 4+4.1+4.2 (review) / Phase 4.3 (ultra)
//
// 起動例 (SKILL.md より):
//   Workflow({ scriptPath: "$HOME/.claude/workflows/forge.js", args: {
//     kind: 'review',            // 'investigate' | 'review' | 'ultra'
//     mode: 'Standard',          // 'Minimum'|'Minimum+'|'Standard'|'Maximum'|'Ultra'
//     target: 'Sources/Foo.swift',
//     language: 'swift',         // 'swift'|'electron'|'node'|'go'|'rails'|'css'|'generic'
//     extraAgents: ['security-auditor'],  // content 検出した条件付き専門家 (任意)
//     agents: undefined,         // 完全上書きしたい場合のみ [{name,reviewer,lens}] を渡す
//     maxRounds: 3,              // ultra のみ
//   }})
//
// 仕様の出典: _common/modes.md (ロスター/モード別動作), _common/agents.md (エージェント),
//             _common/cross-review.md (ペアリング/統合/JSON スキーマ)
// ───────────────────────────────────────────────────────────────────────────

const A = (args && typeof args === 'object') ? args : {}
const KIND = A.kind || 'review'
const MODE = A.mode || 'Standard'
const TARGET = A.target || '(target 未指定)'
const LANGUAGE = A.language || 'swift'
const EXTRA = Array.isArray(A.extraAgents) ? A.extraAgents : []
const MAX_ROUNDS = Number.isInteger(A.maxRounds) ? A.maxRounds : 3

// モード別の動作フラグ (_common/modes.md「各フェーズでの動作」テーブルより)
const DO_CROSS_REVIEW = MODE === 'Minimum+' || MODE === 'Standard' || MODE === 'Maximum'
const DO_INTEGRATE = DO_CROSS_REVIEW // 統合は cross-review があるモードでのみ

// ── 構造化出力スキーマ (_common/cross-review.md「標準 JSON スキーマ」に対応) ──

const FINDINGS_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['agent', 'summary', 'issues'],
  properties: {
    agent: { type: 'string' },
    summary: { type: 'string', description: '1-2 文のサマリー' },
    issues: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['severity', 'category', 'location', 'description', 'suggestion'],
        properties: {
          severity: { type: 'string', enum: ['high', 'medium', 'low'] },
          category: {
            type: 'string',
            description: 'security|performance|accessibility|architecture|maintainability|correctness|consistency|testing|documentation のいずれか',
          },
          location: { type: 'string', description: 'filepath:line 形式。調査フェーズで未確定なら概略' },
          description: { type: 'string' },
          suggestion: { type: 'string' },
        },
      },
    },
    recommendations: { type: 'array', items: { type: 'string' } },
  },
}

const CROSS_VERDICT_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['reviewer', 'verdict'],
  properties: {
    reviewer: { type: 'string' },
    verdict: { type: 'string', enum: ['agree', 'needs_discussion', 'disagree'] },
    agree: { type: 'array', items: { type: 'string' }, description: '✅ 妥当と判断した指摘' },
    needsDiscussion: { type: 'array', items: { type: 'string' }, description: '⚠️ 追加検討が必要' },
    overreach: { type: 'array', items: { type: 'string' }, description: '❌ 過剰反応と判断' },
    additional: {
      type: 'array',
      description: '💡 元レビューが見落とした問題',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['severity', 'category', 'location', 'description'],
        properties: {
          severity: { type: 'string', enum: ['high', 'medium', 'low'] },
          category: { type: 'string' },
          location: { type: 'string' },
          description: { type: 'string' },
        },
      },
    },
  },
}

const INTEGRATED_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['high', 'medium', 'low'],
  properties: {
    summary: { type: 'string' },
    high: { type: 'array', items: integratedItem() },
    medium: { type: 'array', items: integratedItem() },
    low: { type: 'array', items: integratedItem() },
    excluded: {
      type: 'array',
      description: '過剰と判断され除外された指摘',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['title', 'reason'],
        properties: { title: { type: 'string' }, reason: { type: 'string' }, judgedBy: { type: 'string' } },
      },
    },
    conflicts: {
      type: 'array',
      description: 'エージェント間で見解が分かれた点 (main Claude がユーザーに判断を委ねる)',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['title', 'point'],
        properties: {
          title: { type: 'string' },
          for: { type: 'string' },
          against: { type: 'string' },
          point: { type: 'string' },
        },
      },
    },
  },
}

function integratedItem() {
  return {
    type: 'object',
    additionalProperties: false,
    required: ['title', 'location', 'source'],
    properties: {
      title: { type: 'string' },
      location: { type: 'string' },
      category: { type: 'string' },
      source: { type: 'string', description: '指摘元エージェント名' },
      crossReview: { type: 'string', description: 'クロスレビュー判定 (✅/⚠️/❌ + 補足)' },
      suggestion: { type: 'string' },
    },
  }
}

const ULTRA_ROUND_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['agent', 'hasNewFindings'],
  properties: {
    agent: { type: 'string' },
    hasNewFindings: { type: 'boolean', description: '前ラウンドから新たな発見があったか。収束判定に使う' },
    newFindings: { type: 'array', items: { type: 'string' }, description: '🆕 他エージェントの指摘を受けて気づいた点' },
    viewUpdates: { type: 'array', items: { type: 'string' }, description: '🔄 前ラウンドからの見解変更' },
    deepDive: { type: 'array', items: { type: 'string' }, description: '🔍 深掘り分析' },
    agreements: { type: 'array', items: { type: 'string' }, description: '✅ 全員一致点' },
    openQuestions: { type: 'array', items: { type: 'string' }, description: '❓ 未解決の疑問' },
  },
}

const ULTRA_INTEGRATED_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['rootCause', 'high'],
  properties: {
    rootCause: { type: 'string', description: '🎯 全エージェントが合意した確定根本原因' },
    evolution: {
      type: 'array',
      description: '📊 分析の変遷 (ラウンドごと)',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['round', 'finding'],
        properties: { round: { type: 'number' }, finding: { type: 'string' }, change: { type: 'string' } },
      },
    },
    high: { type: 'array', items: integratedItem() },
    openQuestions: { type: 'array', items: { type: 'string' }, description: '❓ 追加調査が必要' },
    sideFindings: { type: 'array', items: { type: 'string' }, description: '💡 副次的な発見' },
  },
}

// ── ロスター解決 (_common/modes.md + agents.md) ──

// 言語別ベースロスター。swift は modes.md の必須6+1、他言語は agents.md「言語別エージェント置換ルール」に準拠。
function baseRoster(mode, language, kind) {
  const lite = mode === 'Minimum' || mode === 'Minimum+'

  if (language === 'go') {
    return lite
      ? ['go-architecture-designer', 'architecture-reviewer', 'test-coverage-advisor']
      : ['go-architecture-designer', 'architecture-reviewer', 'security-auditor', 'test-coverage-advisor', 'Explore']
  }
  if (language === 'rails') {
    return lite
      ? ['rails-domain-designer', 'architecture-reviewer', 'test-coverage-advisor']
      : ['rails-domain-designer', 'architecture-reviewer', 'security-auditor', 'test-coverage-advisor', 'Explore']
  }
  if (language === 'electron' || language === 'node' || language === 'css') {
    // フロントエンド/デスクトップ: agents.md の置換ルール + cross-review.md フロントエンド用ペアリング
    const fe = lite
      ? ['nodejs-expert', 'architecture-reviewer', 'Explore']
      : ['nodejs-expert', 'css-expert', 'Explore', 'architecture-reviewer', 'security-auditor']
    if (language === 'electron') fe.push('electron-expert')
    if (language === 'css' && !fe.includes('css-expert')) fe.push('css-expert')
    return fe
  }

  // swift (デフォルト)
  if (lite) {
    return ['swift-language-expert', 'architecture-reviewer', 'swiftui-test-expert']
  }
  // Standard/Maximum/Ultra: 必須6 + swiftui-performance-expert(Phase4常時)
  const swift = [
    'swift-language-expert',
    'swiftui-macos-designer',
    'Explore',
    'architecture-reviewer',
    'swiftui-test-expert',
    'swiftui-performance-expert',
  ]
  // research-assistant は Phase 1 (investigate) のみ
  if (kind === 'investigate') swift.push('research-assistant')
  return swift
}

function resolveRoster(mode, language, kind, extra, override) {
  if (Array.isArray(override) && override.length) {
    // 完全上書き: skill が {name,reviewer,lens} を解決済みで渡したケース
    return dedupeBy(override.map((o) => (typeof o === 'string' ? { name: o } : o)), (o) => o.name)
  }
  let names = baseRoster(mode, language, kind)
  // Maximum/Ultra 専用追加 (modes.md)
  if (mode === 'Maximum' || mode === 'Ultra') {
    names.push('dependency-analyzer', 'test-coverage-advisor')
  }
  names.push(...extra) // 条件付き専門家 (security-auditor / swift-concurrency-expert / refactoring-patterns / 検出した specialist)
  names = Array.from(new Set(names))
  // 各エージェントにクロスレビュー担当を割り当てる
  const items = names.map((name) => ({ name, ...resolvePairing(name, names, language, mode) }))
  return items
}

// クロスレビューのペアリング (_common/cross-review.md)
function pairingTable(language, mode) {
  if (language === 'electron' || language === 'node' || language === 'css') {
    return {
      'css-expert': { reviewer: 'nodejs-expert', lens: 'CSS の指摘がビルド設定と整合しているか' },
      'nodejs-expert': { reviewer: 'security-auditor', lens: 'Node.js の指摘がセキュリティを考慮しているか' },
      'electron-expert': { reviewer: 'nodejs-expert', lens: 'Electron の指摘が Node.js パターンと整合しているか' },
      'security-auditor': { reviewer: 'nodejs-expert', lens: 'セキュリティ指摘が実装制約と整合しているか' },
      'architecture-reviewer': { reviewer: 'nodejs-expert', lens: '設計が実装パターンを活かしているか' },
      Explore: { reviewer: 'nodejs-expert', lens: '類似コードのカバレッジは十分か' },
    }
  }
  if (language === 'go') {
    return {
      'go-architecture-designer': { reviewer: 'security-auditor', lens: 'Go の指摘がセキュリティを考慮しているか' },
      'architecture-reviewer': { reviewer: 'go-architecture-designer', lens: '設計が Go の言語特性を活かしているか' },
      'security-auditor': { reviewer: 'go-architecture-designer', lens: 'セキュリティ指摘が Go の実装制約と整合しているか' },
    }
  }
  if (language === 'rails') {
    return {
      'rails-domain-designer': { reviewer: 'security-auditor', lens: 'Rails の指摘がセキュリティを考慮しているか' },
      'architecture-reviewer': { reviewer: 'rails-domain-designer', lens: '設計が Rails の規約を活かしているか' },
      'security-auditor': { reviewer: 'rails-domain-designer', lens: 'セキュリティ指摘が Rails の実装制約と整合しているか' },
    }
  }
  // Minimum+ は 3 エージェント専用ペアリング (Explore 不在のため)
  if (mode === 'Minimum+') {
    return {
      'swift-language-expert': { reviewer: 'architecture-reviewer', lens: '言語機能の選択が設計に適合しているか' },
      'architecture-reviewer': { reviewer: 'swift-language-expert', lens: '設計が Swift の言語機能を活かしているか' },
      'swiftui-test-expert': { reviewer: 'architecture-reviewer', lens: 'テスト戦略がアーキテクチャと整合しているか' },
    }
  }
  // Swift/macOS 標準
  return {
    'swift-language-expert': { reviewer: 'architecture-reviewer', lens: '言語機能の選択が設計に適合しているか' },
    'swiftui-macos-designer': { reviewer: 'swiftui-performance-expert', fallback: 'architecture-reviewer', lens: 'UI 設計がパフォーマンスに影響しないか' },
    'architecture-reviewer': { reviewer: 'swift-language-expert', lens: '設計が Swift の言語機能を活かしているか' },
    'swiftui-test-expert': { reviewer: 'Explore', fallback: 'architecture-reviewer', lens: 'テスト戦略が既存パターンと整合しているか' },
    Explore: { reviewer: 'swiftui-test-expert', lens: '特定した類似コードのテストカバレッジは十分か' },
    'research-assistant': { reviewer: 'security-auditor', fallback: 'swift-language-expert', lens: 'ベストプラクティスにセキュリティ懸念はないか' },
    'swiftui-performance-expert': { reviewer: 'swiftui-macos-designer', fallback: 'architecture-reviewer', lens: 'パフォーマンス改善が UX を損なわないか' },
    'swift-concurrency-expert': { reviewer: 'swift-language-expert', lens: '並行設計が言語制約を考慮しているか' },
    'swift-architecture-designer': { reviewer: 'swift-language-expert', lens: '構造変更が言語制約を考慮しているか' },
    'refactoring-patterns': { reviewer: 'architecture-reviewer', lens: 'リファクタリング案がアーキテクチャと整合しているか' },
    'dependency-analyzer': { reviewer: 'architecture-reviewer', lens: '依存分析が設計判断と整合しているか' },
    'test-coverage-advisor': { reviewer: 'swiftui-test-expert', fallback: 'architecture-reviewer', lens: 'カバレッジ提案がテスト戦略と整合しているか' },
    'security-auditor': { reviewer: 'architecture-reviewer', lens: 'セキュリティ指摘が設計と整合しているか' },
  }
}

function resolvePairing(name, roster, language, mode) {
  const table = pairingTable(language, mode)
  const entry = table[name]
  const inRoster = (n) => n && n !== name && roster.includes(n)
  let reviewer
  let lens
  if (entry) {
    lens = entry.lens
    if (inRoster(entry.reviewer)) reviewer = entry.reviewer
    else if (inRoster(entry.fallback)) reviewer = entry.fallback
  }
  if (!reviewer) {
    // フォールバック: ロスター内の別エージェント (architecture-reviewer 優先、無ければ先頭の別エージェント)
    reviewer = inRoster('architecture-reviewer')
      ? 'architecture-reviewer'
      : roster.find((n) => n !== name) || name
    lens = lens || '指摘の妥当性・見落とし・過剰反応・優先度の妥当性を検証する'
  }
  return { reviewer, lens }
}

function dedupeBy(arr, keyFn) {
  const seen = new Set()
  const out = []
  for (const x of arr) {
    const k = keyFn(x)
    if (seen.has(k)) continue
    seen.add(k)
    out.push(x)
  }
  return out
}

// ── プロンプトビルダー (_common/agents.md + cross-review.md のテンプレートを集約) ──

const JSON_NOTE = '発見事項は厳密に JSON スキーマに従って返すこと。location は filepath:line 形式 (調査段階で行が未確定なら概略でよい)。severity は high/medium/low。'

function investigatePrompt(name, target) {
  return [
    `以下のタスクを実装するために、あなたの専門領域 (${name}) から徹底的に事前調査してください。`,
    '',
    `タスク: ${target}`,
    '',
    '【必須の観点】',
    '- 関連する言語機能 / 設計パターン / フレームワーク知識',
    '- 既存コードベースの類似機能・再利用可能な実装 (あればファイルパスと行番号を明示)',
    '- 実装方針の提案と、想定される落とし穴・リスク',
    '- 影響範囲 (変更が必要なファイル、依存関係、既存テストへの影響)',
    '',
    'プロジェクトの CLAUDE.md や規約ドキュメントがあれば、それに準拠する方針を提案すること。',
    JSON_NOTE,
  ].join('\n')
}

function reviewPrompt(name, target) {
  return [
    `以下を、あなたの専門領域 (${name}) からコードレビューしてください。`,
    '',
    `対象: ${target}`,
    '',
    '【観点】',
    '- 正確性・ロジックエラー・エラーハンドリング・境界値',
    '- 設計/責務分離/依存方向/テスタビリティ/パフォーマンス (専門領域に応じて)',
    '- 既存コードとの一貫性 (命名・構造・配置)',
    '- セキュリティ / メモリ管理 / 並行処理 (該当する場合)',
    '',
    '**プロジェクトルール**: CLAUDE.md や規約ドキュメントにルールが定義されていれば準拠を検証すること。',
    '**修正案は構造的に**: 場当たり的な条件分岐の追加ではなく、設計上の前提を是正する方針を優先すること。',
    JSON_NOTE,
  ].join('\n')
}

function crossReviewPrompt(originalAgent, lens, phase, findingsJson) {
  const header =
    phase === 'investigate'
      ? `以下は ${originalAgent} の事前調査結果です。「${lens}」の観点から検証してください。`
      : `以下は ${originalAgent} のコードレビュー結果です。「${lens}」の観点から検証してください。`
  return [
    header,
    '',
    '【検証対象 (JSON)】',
    '```json',
    JSON.stringify(findingsJson, null, 2),
    '```',
    '',
    '【検証項目】',
    '1. 指摘の妥当性: 各指摘は正当か、過剰反応ではないか',
    '2. 見落とし: 重要な問題が見落とされていないか',
    '3. 修正案の適切性: 提案された修正は副作用を起こさないか',
    '4. 優先度の妥当性: severity の判断は適切か',
    '5. 構造的修正か: パッチワークでなく前提の是正になっているか',
    '',
    'verdict は agree (全体に妥当) / needs_discussion (要検討あり) / disagree (過剰が多い) のいずれか。',
    'agree[]=✅妥当 / needsDiscussion[]=⚠️要検討 / overreach[]=❌過剰 / additional[]=💡見落とし、を JSON スキーマに従って返すこと。',
  ].join('\n')
}

function integratePrompt(phase, reviewed) {
  const label = phase === 'investigate' ? '事前調査' : 'コードレビュー'
  return [
    `以下は Phase の${label}結果と、それぞれのクロスレビュー結果です。これらを統合し、main Claude に報告する最終結果を作成してください。`,
    '',
    '【統合ルール】(_common/cross-review.md)',
    '1. 重複排除: same file + same line + same category は重複とみなし severity が高い方を採用',
    '2. ❌ 過剰と判断された指摘は excluded[] に理由付きで移動',
    '3. ⚠️ 要検討は該当 item の crossReview に注釈',
    '4. 💡 追加指摘 (additional) は通常の指摘として取り込む',
    '5. 出典 (source = エージェント名) を保持',
    '6. severity 順にソート (high → medium → low)',
    '7. エージェント間で矛盾する指摘は conflicts[] に両論併記 (独自判断で潰さない)',
    '',
    '【統合対象 (各エージェントの findings + cross-review verdict)】',
    '```json',
    JSON.stringify(reviewed, null, 2),
    '```',
    '',
    'INTEGRATED スキーマに従って返すこと。',
  ].join('\n')
}

function ultraAnalyzePrompt(name, target) {
  return [
    `以下の問題を、あなたの専門領域 (${name}) から独立して分析してください (Ultra Round 1)。`,
    '',
    `問題: ${target}`,
    '',
    '根本原因の仮説、観測すべき事実、関連するコード箇所を挙げてください。',
    'openQuestions には、さらに調査が必要な未解決の疑問を列挙すること。Round 1 では hasNewFindings=true。',
    'ULTRA_ROUND スキーマに従って返すこと。',
  ].join('\n')
}

function ultraReanalyzePrompt(name, round, target, priorAll, priorSelf) {
  return [
    `以下は他の専門家エージェントの分析結果 (前ラウンド) です。これらを踏まえ、あなたの専門領域 (${name}) から再分析してください (Ultra Round ${round})。`,
    '',
    `問題: ${target}`,
    '',
    '【他エージェントを含む前ラウンドの全分析 (JSON)】',
    '```json',
    JSON.stringify(priorAll, null, 2),
    '```',
    '',
    '【あなた自身の前ラウンド分析】',
    '```json',
    JSON.stringify(priorSelf || {}, null, 2),
    '```',
    '',
    '【再分析の観点】',
    '1. 新たな発見: 他エージェントの指摘を受けて気づいた点 (newFindings)',
    '2. 見解の変化: 前ラウンドから修正すべき点 (viewUpdates)',
    '3. 深掘り: 他エージェントの指摘をさらに発展させた分析 (deepDive)',
    '4. 合意事項: 全員が同意している点 (agreements)',
    '5. 未解決の疑問 (openQuestions)',
    '',
    '**重要**: 前ラウンドから本当に新しい発見・見解変更がある場合のみ hasNewFindings=true。なければ false (収束判定に使う)。',
    'ULTRA_ROUND スキーマに従って返すこと。',
  ].join('\n')
}

function ultraIntegratePrompt(rounds, target) {
  return [
    `以下は問題「${target}」に対する全 ${rounds.length} ラウンドの反復分析結果です。統合して最終的な分析結果を作成してください。`,
    '',
    '【統合ルール】',
    '1. ラウンドを重ねて収束した結論を優先',
    '2. 複数エージェントが同意した指摘を高優先度に',
    '3. 最終ラウンドで未解決の点は openQuestions に記録',
    '4. 仮説の変遷 (どう深まったか) を evolution に記録',
    '',
    '【全ラウンドの結果 (JSON)】',
    '```json',
    JSON.stringify(rounds, null, 2),
    '```',
    '',
    'ULTRA_INTEGRATED スキーマに従って返すこと。',
  ].join('\n')
}

// ── 実行フロー ──

const PHASE_LABEL = KIND === 'investigate' ? 'Investigate' : KIND === 'ultra' ? 'Ultra Rounds' : 'Review'

const roster = resolveRoster(MODE, LANGUAGE, KIND, EXTRA, A.agents)
if (!roster.length) {
  return { error: 'ロスターが空です', kind: KIND, mode: MODE, language: LANGUAGE }
}
log(`forge ${KIND} / mode=${MODE} / lang=${LANGUAGE} / ${roster.length}体: ${roster.map((r) => r.name).join(', ')}`)
log(`cross-review=${DO_CROSS_REVIEW} integrate=${DO_INTEGRATE}`)

// ── Ultra: 反復並列思考 (Phase 4.3) ──
if (KIND === 'ultra') {
  // roundsRaw は null を保持する (= roster と index を 1:1 対応させる)。
  // filter で穴を詰めると prior[i] が別エージェントの結果を指すズレが起きるため、index 整合用は raw を使い、
  // 収束判定・統合・返却にだけ filter(Boolean) を適用する。
  const roundsRaw = []
  phase('Ultra Rounds')
  const round1 = await parallel(
    roster.map((r) => () =>
      agent(ultraAnalyzePrompt(r.name, TARGET), {
        label: `ultra-r1:${r.name}`,
        phase: 'Ultra Rounds',
        agentType: r.name,
        schema: ULTRA_ROUND_SCHEMA,
      }),
    ),
  )
  roundsRaw.push(round1)

  for (let r = 2; r <= MAX_ROUNDS; r++) {
    const prior = roundsRaw[roundsRaw.length - 1] // roster と index 対応 (null 含む)
    const priorAll = prior.filter(Boolean) // 他エージェント分析として渡すぶんはクリーンに
    const next = await parallel(
      roster.map((item, i) => () =>
        agent(ultraReanalyzePrompt(item.name, r, TARGET, priorAll, prior[i]), {
          // prior[i] は roster[i] 本人の前ラウンド結果 (失敗していれば null → prompt 側で {} にフォールバック)
          label: `ultra-r${r}:${item.name}`,
          phase: 'Ultra Rounds',
          agentType: item.name,
          schema: ULTRA_ROUND_SCHEMA,
        }),
      ),
    )
    roundsRaw.push(next)
    const answered = next.filter(Boolean)
    const converged = answered.length > 0 && answered.every((x) => x.hasNewFindings === false)
    log(`Ultra Round ${r}: ${answered.length}体応答, 収束=${converged}`)
    if (converged) break
  }

  const rounds = roundsRaw.map((rd) => rd.filter(Boolean)) // 統合/返却はクリーンな配列
  phase('Ultra Integrate')
  const integrated = await agent(ultraIntegratePrompt(rounds, TARGET), {
    label: 'ultra-integrate',
    phase: 'Ultra Integrate',
    schema: ULTRA_INTEGRATED_SCHEMA,
  })
  return { kind: 'ultra', mode: MODE, roundsRun: rounds.length, rounds, integrated }
}

// ── investigate / review 共通: 専門家並行 → (クロスレビュー) → (統合) ──
const isInvestigate = KIND === 'investigate'
const buildPrompt = isInvestigate ? investigatePrompt : reviewPrompt
const findPhase = isInvestigate ? 'Investigate' : 'Review'

let reviewed

if (DO_CROSS_REVIEW) {
  // pipeline: 各専門家のレビュー完了次第、ペア相手がクロスレビュー (barrier なし)
  reviewed = (
    await pipeline(
      roster,
      (item) =>
        agent(buildPrompt(item.name, TARGET), {
          label: `${isInvestigate ? 'inv' : 'rev'}:${item.name}`,
          phase: findPhase,
          agentType: item.name,
          schema: FINDINGS_SCHEMA,
        }),
      (findings, item) => {
        if (!findings) return null
        return agent(crossReviewPrompt(item.name, item.lens, KIND, findings), {
          label: `xrev:${item.name}→${item.reviewer}`,
          phase: 'Cross-Review',
          agentType: item.reviewer,
          schema: CROSS_VERDICT_SCHEMA,
        }).then((verdict) => ({ agent: item.name, reviewer: item.reviewer, findings, verdict }))
      },
    )
  ).filter(Boolean)
} else {
  // Minimum: クロスレビューなし。専門家の出力をそのまま使う (modes.md)
  const found = (
    await parallel(
      roster.map((item) => () =>
        agent(buildPrompt(item.name, TARGET), {
          label: `${isInvestigate ? 'inv' : 'rev'}:${item.name}`,
          phase: findPhase,
          agentType: item.name,
          schema: FINDINGS_SCHEMA,
        }),
      ),
    )
  ).filter(Boolean)
  reviewed = found.map((findings) => ({ agent: findings.agent, findings, verdict: null }))
}

if (!DO_INTEGRATE) {
  // Minimum: 統合エージェントを起動せず raw を返す (main Claude が直接マージ)
  return {
    kind: KIND,
    mode: MODE,
    integrated: null,
    raw: reviewed.map((x) => x.findings),
  }
}

// 統合 (barrier 後): 重複排除・矛盾検出・優先度ソート
phase('Integrate')
const integrated = await agent(integratePrompt(KIND, reviewed), {
  label: 'integrate',
  phase: 'Integrate',
  schema: INTEGRATED_SCHEMA,
})

return { kind: KIND, mode: MODE, agents: roster.map((r) => r.name), integrated, reviewed }
