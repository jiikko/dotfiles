import { test, expect } from 'claude-code/testing'
import { parseAnsi } from '../hooks/ansi'
import { displayName, statusInput } from '../hooks/input'

test('ANSI の色・背景・太字を区切りの属性に置き換え、行で分ける', () => {
  const out = '\x1b[1m~/dotfiles\x1b[0m \x1b[30m\x1b[42m[master]\x1b[0m\n5h \x1b[5;1;31m46%\x1b[0m\n'
  expect(parseAnsi(out)).toEqual([
    [{ text: '~/dotfiles', bold: true }, { text: ' ' }, { text: '[master]', color: '#2d333b', backgroundColor: '#57ab5a' }],
    [{ text: '5h ' }, { text: '46%', bold: true, color: '#e5534b' }],
  ])
})

test('色の無い出力はそのまま 1 区切り', () => {
  expect(parseAnsi('plain\n')).toEqual([[{ text: 'plain' }]])
})

test('モデルの id を CLI の display_name の形にする', () => {
  expect(displayName('claude-opus-5-5')).toBe('Opus 5.5')
  expect(displayName('claude-fable-5-1')).toBe('Fable 5.1')
  expect(displayName('claude-haiku-4-5-20251001')).toBe('Haiku 4.5')
  expect(displayName('custom-model')).toBe('custom-model')
})

test('CLI の statusLine と同じ形の JSON を組む (リセット時刻はエポック秒)', () => {
  const j = statusInput({
    cwd: '/w',
    modelId: 'claude-opus-5-5',
    usage: {
      context: { tokens: 182000, window: 1000000, percent: 18 },
      rateLimits: [
        { kind: 'five_hour', percentUsed: 46, resetsAt: '2026-10-02T07:40:00Z' },
        { kind: 'seven_day', percentUsed: 15 },
      ],
    },
    effort: 'xhigh',
    sessionId: 's1',
  })
  expect(j).toEqual({
    cwd: '/w',
    workspace: { current_dir: '/w' },
    model: { display_name: 'Opus 5.5' },
    rate_limits: { five_hour: { used_percentage: 46, resets_at: 1790926800 }, seven_day: { used_percentage: 15 } },
    context_window: { total_input_tokens: 182000, context_window_size: 1000000, used_percentage: 18 },
    effort: { level: 'xhigh' },
    session_id: 's1',
    transcript_path: '',
  })
})
