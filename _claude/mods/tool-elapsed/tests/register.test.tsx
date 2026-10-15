import { expect, mock, test } from 'claude-code/testing'
import type { Engine, MockClock } from 'claude-code/testing'
import type { On } from 'claude-code'

const NOW = 1_000_000
const BAND = { component: 'AbovePrompt', props: { hasSurvey: false, isWorking: true, maxRows: 10, bodyColumns: 100, scroll: { offset: 0, bodyRows: 9 }, view: {} } } as const

type Floor = { clock: MockClock; toasts: string[]; finish: () => void }

const floor = (on: On): Floor => {
  const pending: Array<() => void> = []
  // finish(): 底で待っている tool.call を全部終わらせる (長い Bash の役)
  const f: Floor = { clock: mock.clock(on, { now: NOW }), toasts: [], finish: () => pending.splice(0).forEach(r => r()) }
  on('ui.toast', (_$, e) => {
    f.toasts.push(e.text)
    return { value: undefined }
  })
  on('ui.render', () => ({ type: 'engine' as const, ref: 0 }))
  on('session.start', () => ({ cwd: '/w' }))
  on('tool.call', () => new Promise<{ result: { stdout: string; stderr: string } }>(resolve => pending.push(() => resolve({ result: { stdout: 'ok', stderr: '' } }))))
  return f
}

const band = ($: Engine) => $.ui.mount({ plugin: 'tool-elapsed', surface: 'terminal', ...BAND })

test('20 秒未満は帯に出ず、越えると経過時間つきで出て、終わると消える', async ($, on) => {
  const f = floor(on)
  const call = $.tool.call({ tool: 'Bash', command: 'codex exec review', description: 'codex review' })
  await f.clock.advance(5_000)
  let ui = await band($)
  expect(await ui.find({ type: 'Text', text: /実行中/ })).toBeUndefined()
  await ui.unmount()

  await f.clock.advance(25_000) // 計 30 秒
  ui = await band($)
  expect(await ui.find({ type: 'Text', text: /実行中 1 件/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /^00:30 $/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /Bash: codex review/ })).toBeDefined()
  await ui.unmount()

  f.finish()
  await call
  await f.clock.advance(1_000)
  ui = await band($)
  expect(await ui.find({ type: 'Text', text: /実行中/ })).toBeUndefined()
  await ui.unmount()
})

test('同じ mount が tick の購読で描き直される (mount し直さない)', async ($, on) => {
  const f = floor(on)
  const call = $.tool.call({ tool: 'Bash', command: 'swift test' })
  await f.clock.advance(30_000)
  const ui = await band($)
  expect(await ui.find({ type: 'Text', text: /^00:30 $/ })).toBeDefined()
  await f.clock.advance(15_000)
  expect(await ui.find({ type: 'Text', text: /^00:45 $/ })).toBeDefined()
  f.finish()
  await call
  await f.clock.advance(1_000)
  expect(await ui.find({ type: 'Text', text: /実行中/ })).toBeUndefined()
  await ui.unmount()
})

test('並列に終わっても記録が残らない (10 本同時に完了)', async ($, on) => {
  const f = floor(on)
  const calls = Array.from({ length: 10 }, (_, i) => $.tool.call({ tool: 'Bash', command: `job ${i}` }))
  await f.clock.advance(30_000)
  let ui = await band($)
  expect(await ui.find({ type: 'Text', text: /実行中 10 件/ })).toBeDefined()
  await ui.unmount()
  f.finish()
  await Promise.all(calls)
  await f.clock.advance(1_000)
  ui = await band($)
  expect(await ui.find({ type: 'Text', text: /実行中/ })).toBeUndefined()
  await ui.unmount()
})

// mock の時計は進めた間の tick を全部発火させる (飛ばす口は無い) ので、この 1 本は 1 秒の timer を約 600 回回す。単独で 3.6〜4.0 秒かかり
// (2026-10-15 実測・2.1.300)、既定の上限 5 秒では make test の並列の負荷の中で切れていた。上限はハングの安全網で判定ではないので、
// 仕事の量に見合う長さにする (判定は toast の中身と回数)
test('5 分ごとに toast が出る (10 分で 2 回)', { timeoutMs: 30_000 }, async ($, on) => {
  const f = floor(on)
  const call = $.tool.call({ tool: 'Bash', command: 'make test' })
  await f.clock.advance(4 * 60_000 + 59_000)
  expect(f.toasts).toEqual([])
  await f.clock.advance(1_000)
  expect(f.toasts).toEqual(['⏳ Bash: make test: 5 分経過'])
  await f.clock.advance(5 * 60_000)
  expect(f.toasts).toEqual(['⏳ Bash: make test: 5 分経過', '⏳ Bash: make test: 10 分経過'])
  f.finish()
  await call
})

test('tool.call の結果は素通し (底の result がそのまま返る)', async ($, on) => {
  const f = floor(on)
  const call = $.tool.call({ tool: 'Bash', command: 'echo' })
  await f.clock.advance(1) // 底の hook が走って finish が差し替わるまで進める
  f.finish()
  expect(await call).toEqual({ result: { stdout: 'ok', stderr: '' } })
})
