import { expect, mock, test } from 'claude-code/testing'
import type { On } from 'claude-code'

// テストの $ の下には engine の実装が無いので、配送の最下段と時計を自分で敷く。
const floor = (on: On) => {
  mock.clock(on, { now: Date.UTC(2026, 9, 7, 3, 4) })
  on('session.receive', (_$, e) => ({ text: e.text }))
  on('session.send', () => ({ isDelivered: true as const }))
  // 帯に出すものが無いとき mod は next(e) で engine に譲る。その engine の答え (何も描かない)。
  on('ui.render', () => ({ type: 'engine' as const, ref: 0 }))
}

const BAND = {
  component: 'AbovePrompt',
  props: {
    hasSurvey: false,
    isWorking: false,
    maxRows: 10,
    bodyColumns: 100,
    scroll: { offset: 0, bodyRows: 9 },
    view: {},
  },
} as const

test('受信した peer message が帯に出て、既読で色が落ちる', async ($, on) => {
  floor(on)
  await $.session.receive({ origin: { kind: 'peer' }, text: 'issue 803 は私が持っています\n進めてよいですか' })
  await $.session.send({ to: 'obaket-2', text: 'どうぞ', origin: { kind: 'model' } as never })

  const ui = await $.ui.mount({ plugin: 'peer-inbox', surface: 'terminal', ...BAND })
  expect(await ui.find({ type: 'Text', text: /他セッション 2 件 \(未読 1\)/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /issue 803 は私が持っています 進めてよいですか/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /⇨ obaket-2/ })).toBeDefined()

  await ui.press({ key: 'read' })
  expect(await ui.find({ type: 'Text', text: /未読/ })).toBeUndefined()
  await ui.unmount()
})

test('relay の通知 (task-notification) は帯に出ない', async ($, on) => {
  floor(on)
  await $.session.receive({ origin: { kind: 'task-notification' }, text: 'CI finished' })
  const ui = await $.ui.mount({ plugin: 'peer-inbox', surface: 'terminal', ...BAND })
  expect(await ui.find({ type: 'Text', text: /他セッション/ })).toBeUndefined()
  await ui.unmount()
})

test('隠した帯は次の受信で戻る', async ($, on) => {
  floor(on)
  await $.session.receive({ origin: { kind: 'peer' }, text: 'one' })
  const ui = await $.ui.mount({ plugin: 'peer-inbox', surface: 'terminal', ...BAND })
  await ui.press({ key: 'hide' })
  expect(await ui.find({ type: 'Text', text: /他セッション/ })).toBeUndefined()
  await $.session.receive({ origin: { kind: 'peer' }, text: 'two' })
  expect(await ui.find({ type: 'Text', text: /他セッション 2 件/ })).toBeDefined()
  await ui.unmount()
})
