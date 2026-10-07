import { expect, mock, test } from 'claude-code/testing'
import type { Engine, MockClock } from 'claude-code/testing'
import type { On } from 'claude-code'

const NOW = Date.UTC(2026, 9, 7, 8, 0, 0)
const RESET_ISO = new Date(NOW + 3_600_000).toISOString()
const SID = 'sess-1'

type Floor = { env: Record<string, string | undefined>; status: Array<string | undefined>; file: string | null; clock: MockClock; hookRuns: number; denyStatus: boolean }

// テストの $ の下には engine の実装が無いので、mod が呼ぶ口を自分で敷く
const floor = (on: On, file: string | null, now = NOW): Floor => {
  const f: Floor = { env: {}, status: [], file, clock: mock.clock(on, { now }), hookRuns: 0, denyStatus: false }
  mock.env(on, { HOME: '/home/t', XDG_CACHE_HOME: '/cache' })
  on('env.set', (_$, e) => {
    f.env[e.name] = e.value
    return { value: undefined }
  })
  on('fs.read', (_$, e) => (f.file !== null && e.path === '/cache/glog/claude-rate-limits.json' ? { value: f.file } : { deny: 'ENOENT' }))
  on('ui.status', (_$, e) => {
    if (f.denyStatus) return { deny: 'status refused' }
    f.status.push(e.text)
    return { value: undefined }
  })
  on('session.start', () => ({ cwd: '/w' }))
  // settings の hook の役: mod が next(e) で下へ進めたときだけ走る (進めなければ本番では hook が呼ばれず沈黙する)
  on('classic.UserPromptSubmit', () => {
    f.hookRuns += 1
    return {}
  })
  on('session.measure', (_$, e) => ({ changed: e.changed }))
  return f
}

const fileOver = JSON.stringify({ observedAt: Math.floor(NOW / 1000) - 30, five_hour: { used_percentage: 90, resets_at: Math.floor(NOW / 1000) + 3600 }, seven_day: null, version: 'x' })
const fileUnder = JSON.stringify({ observedAt: Math.floor(NOW / 1000) - 30, five_hour: { used_percentage: 40, resets_at: Math.floor(NOW / 1000) + 3600 }, seven_day: null, version: 'x' })
const fileStale = JSON.stringify({ observedAt: Math.floor(NOW / 1000) - 20 * 60, five_hour: { used_percentage: 90, resets_at: Math.floor(NOW / 1000) + 3600 }, seven_day: null, version: 'x' })

const submit = ($: Engine) => $.classic.UserPromptSubmit({ prompt: 'hi', session_id: SID })

test('file が超過: 注入し、status を出し、印を session_id:時刻 で打つ', async ($, on) => {
  const f = floor(on, fileOver)
  const r = await submit($)
  expect(r.additionalContext?.join('\n')).toContain('claude 5h 90% (')
  expect(f.status).toEqual(['🚨 5h 90%'])
  expect(f.env['DOTFILES_MOD_RATELIMIT_WARN']).toBe(`${SID}:${NOW}`)
  expect(f.hookRuns).toBe(1)
})

test('file が未満: 注入せず、status を消し、印は打つ (判定できた)', async ($, on) => {
  const f = floor(on, fileUnder)
  const r = await submit($)
  expect(r.additionalContext).toBeUndefined()
  expect(f.status).toEqual([undefined]) // 消す呼び出しが 1 回ある (呼ばないのとは違う)
  expect(f.env['DOTFILES_MOD_RATELIMIT_WARN']).toBe(`${SID}:${NOW}`)
  expect(f.hookRuns).toBe(1)
})

test('判定できない (file 無し・live 無し): 印を打たず hook に任せる', async ($, on) => {
  const f = floor(on, null)
  const r = await submit($)
  expect(r.additionalContext).toBeUndefined()
  expect(f.env['DOTFILES_MOD_RATELIMIT_WARN']).toBeUndefined()
  expect(f.hookRuns).toBe(1) // next(e) で settings の hook まで進めている (進めずに {} を返すと本番では沈黙)
})

test('file が 15 分以上前: 使わない = 判定できない', async ($, on) => {
  const f = floor(on, fileStale)
  await submit($)
  expect(f.env['DOTFILES_MOD_RATELIMIT_WARN']).toBeUndefined()
})

test('live (measure) が超過: file が無くても注入し、measure の時点で status が出る', async ($, on) => {
  const f = floor(on, null)
  await $.session.measure({ context: { tokens: 1, window: 200000, percent: 0 }, rateLimits: [{ kind: 'five_hour', percentUsed: 88, resetsAt: RESET_ISO }], changed: ['rateLimits'] })
  expect(f.status.at(-1)).toBe('🚨 5h 88%')
  const r = await submit($)
  expect(r.additionalContext?.join('\n')).toContain('claude 5h 88% (')
})

test('live より新しい file を採る (別 session の観測が勝つ)', async ($, on) => {
  const f = floor(on, fileOver, NOW - 10 * 60_000)
  await $.session.measure({ context: { tokens: 1, window: 200000, percent: 0 }, rateLimits: [{ kind: 'five_hour', percentUsed: 40, resetsAt: RESET_ISO }], changed: ['rateLimits'] })
  await f.clock.advance(10 * 60_000)
  const r = await submit($)
  expect(r.additionalContext?.join('\n')).toContain('claude 5h 90% (')
  expect(f.status.at(-1)).toBe('🚨 5h 90%')
})

test('measure が空の rateLimits を運んだら、前の live の超過を消す', async ($, on) => {
  const f = floor(on, null)
  await $.session.measure({ context: { tokens: 1, window: 200000, percent: 0 }, rateLimits: [{ kind: 'five_hour', percentUsed: 90, resetsAt: RESET_ISO }], changed: ['rateLimits'] })
  expect(f.status.at(-1)).toBe('🚨 5h 90%')
  await $.session.measure({ context: { tokens: 2, window: 200000, percent: 0 }, rateLimits: [], changed: ['rateLimits'] })
  expect(f.status).toEqual(['🚨 5h 90%', undefined]) // 空の measure の直後に表示も消える
  const r = await submit($)
  expect(r.additionalContext).toBeUndefined()
  expect(f.env['DOTFILES_MOD_RATELIMIT_WARN']).toBeUndefined() // 判定できないので hook に任せる
})

test('observed 後にリセットを過ぎた窓は超過なし', async ($, on) => {
  const past = JSON.stringify({ observedAt: Math.floor(NOW / 1000) - 30, five_hour: { used_percentage: 95, resets_at: Math.floor(NOW / 1000) - 1 }, seven_day: null, version: 'x' })
  const f = floor(on, past)
  const r = await submit($)
  expect(r.additionalContext).toBeUndefined()
  expect(f.env['DOTFILES_MOD_RATELIMIT_WARN']).toBe(`${SID}:${NOW}`)
})

test('60 秒の timer が status を見直し、$ の口が拒否しても次の周は動く (未処理 reject にしない)', async ($, on) => {
  const f = floor(on, fileOver)
  await $.session.start({ cwd: '/w', isInteractive: true, surface: 'terminal' } as never)
  f.denyStatus = true
  await f.clock.advance(60_000) // この周の $.ui.status は拒否される
  expect(f.status).toEqual([])
  f.denyStatus = false
  await f.clock.advance(60_000)
  expect(f.status).toEqual(['🚨 5h 90%'])
})

