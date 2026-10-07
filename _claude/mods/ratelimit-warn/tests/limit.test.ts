import { describe, expect, test } from 'claude-code/testing'

import { FILE_MAX_AGE_MS, LIVE_MAX_AGE_MS, THRESHOLD, formatReset, fromLive, judge, message, parseFile, pick } from '../hooks/limit'

// ローカル時刻で組む (formatReset の期待値をタイムゾーンに依らず文字列で固定するため)
const NOW = new Date(2026, 9, 7, 17, 0, 0).getTime()
const RESET = new Date(2026, 9, 7, 19, 0, 0).getTime()

const file = (over: Partial<{ observedAt: number; five_hour: unknown }>) =>
  JSON.stringify({ observedAt: Math.floor(NOW / 1000) - 60, five_hour: { used_percentage: 85, resets_at: Math.floor(RESET / 1000) }, seven_day: null, version: 'x', ...over })

describe('parseFile (statusline の claude-rate-limits.json)', () => {
  test('新しい観測は使える', () => {
    expect(parseFile(file({}), NOW)).toEqual({ percent: 85, resetsAtMs: Math.floor(RESET / 1000) * 1000, observedAtMs: (Math.floor(NOW / 1000) - 60) * 1000, source: 'file' })
  })
  test('15 分以上前・未来・壊れた JSON は使えない', () => {
    expect(parseFile(file({ observedAt: Math.floor((NOW - FILE_MAX_AGE_MS) / 1000) }), NOW)).toBe(null)
    expect(parseFile(file({ observedAt: Math.floor(NOW / 1000) + 5 }), NOW)).toBe(null)
    expect(parseFile('{', NOW)).toBe(null)
  })
  test('five_hour が null = リセットで落とされた窓は 0%', () => {
    expect(parseFile(file({ five_hour: null }), NOW)?.percent).toBe(0)
    expect(parseFile(file({ five_hour: { used_percentage: null, resets_at: null } }), NOW)?.resetsAtMs).toBe(null)
  })
  test('使用率があるのにリセット時刻が無い形は使わない', () => {
    expect(parseFile(file({ five_hour: { used_percentage: 90, resets_at: null } }), NOW)).toBe(null)
  })
})

describe('fromLive ($.session.usage().rateLimits)', () => {
  test('five_hour を取り、resetsAt を ms にする', () => {
    // 79.6 は切り捨てて 79 (statusline と同じ)。四捨五入すると 80 になり、旧経路では出なかった警告が出る (658 の red team P2)
    expect(fromLive([{ kind: 'seven_day', percentUsed: 97 }, { kind: 'five_hour', percentUsed: 79.6, resetsAt: new Date(RESET).toISOString() }], NOW))
      .toEqual({ percent: 79, resetsAtMs: RESET, observedAtMs: NOW, source: 'live' })
  })
  test('five_hour が無い / resetsAt が無い → null', () => {
    expect(fromLive([{ kind: 'seven_day', percentUsed: 97 }], NOW)).toBe(null)
    expect(fromLive([{ kind: 'five_hour', percentUsed: 90 }], NOW)).toBe(null)
  })
})

describe('pick (使える観測の新しい方)', () => {
  const live = (ago: number, percent = 70) => ({ percent, resetsAtMs: RESET, observedAtMs: NOW - ago, source: 'live' as const })
  const f = (ago: number, percent = 90) => ({ percent, resetsAtMs: RESET, observedAtMs: NOW - ago, source: 'file' as const })
  test('自分の 20 分前の live より、別 session が 1 分前に書いた file を採る', () => {
    expect(pick(live(20 * 60_000), f(60_000), NOW)?.source).toBe('file')
  })
  test('1 分前の live は 5 分前の file より新しい', () => {
    expect(pick(live(60_000), f(5 * 60_000), NOW)?.source).toBe('live')
  })
  test('live は 30 分、file は 15 分で使えなくなる。両方切れたら null', () => {
    expect(pick(live(LIVE_MAX_AGE_MS), f(FILE_MAX_AGE_MS), NOW)).toBe(null)
    expect(pick(live(LIVE_MAX_AGE_MS - 1), null, NOW)?.source).toBe('live')
    expect(pick(null, f(FILE_MAX_AGE_MS - 1), NOW)?.source).toBe('file')
  })
})

describe('judge', () => {
  test(`閾値 ${THRESHOLD}% は >= で超過`, () => {
    expect(judge({ percent: THRESHOLD, resetsAtMs: RESET, observedAtMs: NOW, source: 'live' }, NOW).over).toBe(true)
    expect(judge({ percent: THRESHOLD - 1, resetsAtMs: RESET, observedAtMs: NOW, source: 'live' }, NOW).over).toBe(false)
  })
  test('観測の後にリセットを過ぎた窓は 0% で超過なし', () => {
    expect(judge({ percent: 95, resetsAtMs: NOW - 1, observedAtMs: NOW - 60_000, source: 'file' }, NOW)).toEqual({ over: false, percent: 0, resetsAtMs: NOW - 1 })
  })
})

describe('message / formatReset (hook の printf と同じ文)', () => {
  test('3 行で、2 行目が claude 5h NN% (HH:MM にリセット)', () => {
    const m = message({ over: true, percent: 85, resetsAtMs: RESET, observedAtMs: NOW, source: 'live' } as never, NOW)
    const lines = m.split('\n')
    expect(lines).toHaveLength(3)
    expect(lines[0]).toBe('🚨 Claude の 5h 枠が閾値を超えている:')
    expect(lines[1]).toBe('claude 5h 85% (19:00 にリセット)')
    expect(lines[2]).toContain('subagent-model-tiering.md の「枠の残量」')
  })
  test('翌日以降のリセットは M/D HH:MM', () => {
    expect(formatReset(new Date(2026, 9, 8, 3, 5, 0).getTime(), NOW)).toBe('10/8 03:05')
    expect(formatReset(new Date(2026, 9, 7, 23, 59, 0).getTime(), NOW)).toBe('23:59')
  })
})
