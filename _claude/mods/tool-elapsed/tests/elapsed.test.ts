import { describe, expect, test } from 'claude-code/testing'

import { SHOW_AFTER_MS, TOAST_EVERY_MS, dueToast, formatElapsed, labelOf, visible } from '../hooks/elapsed'

const run = (startedAt: number, toasts = 0) => ({ id: 'a', tool: 'Bash', label: 'Bash: codex review', startedAt, toasts })

describe('labelOf', () => {
  test('Bash は description、無ければ command', () => {
    expect(labelOf('Bash', { command: 'codex exec', description: 'codex review' })).toBe('Bash: codex review')
    expect(labelOf('Bash', { command: 'make   test\n' })).toBe('Bash: make test')
    expect(labelOf('Read', { file_path: '/x' })).toBe('Read')
    expect(labelOf('Agent', { subagent_type: 'Explore', description: 'find x' })).toBe('Agent (Explore): find x')
  })
})

describe('formatElapsed', () => {
  test('mm:ss と h:mm:ss', () => {
    expect(formatElapsed(0)).toBe('00:00')
    expect(formatElapsed(65_000)).toBe('01:05')
    expect(formatElapsed(3_600_000 + 61_000)).toBe('1:01:01')
  })
})

describe('visible / dueToast', () => {
  test(`${SHOW_AFTER_MS / 1000} 秒未満は出さず、長い順に並ぶ`, () => {
    const now = 100_000
    const runs = { a: { ...run(now - SHOW_AFTER_MS + 1), id: 'a' }, b: { ...run(now - SHOW_AFTER_MS), id: 'b' }, c: { ...run(now - 90_000), id: 'c' } }
    expect(visible(Object.values(runs), now).map(r => r.id)).toEqual(['c', 'b'])
  })
  test('5 分の節目を越えたら toast、越えるまで null、越えた回数を toasts に持つ', () => {
    expect(dueToast(run(0), TOAST_EVERY_MS - 1)).toBe(null)
    expect(dueToast(run(0), TOAST_EVERY_MS)).toEqual({ text: '⏳ Bash: codex review: 5 分経過', toasts: 1 })
    expect(dueToast(run(0, 1), TOAST_EVERY_MS * 2 - 1)).toBe(null)
    expect(dueToast(run(0, 1), TOAST_EVERY_MS * 2 + 30_000)).toEqual({ text: '⏳ Bash: codex review: 10 分経過', toasts: 2 })
  })
})
