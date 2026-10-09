import { expect, mock, test } from 'claude-code/testing'
import type { On } from 'claude-code'

// テストの $ の下には engine の実装が無いので、配送の最下段と時計を自分で敷く。
const floor = (on: On, deliver = true) => {
  mock.clock(on, { now: Date.UTC(2026, 9, 7, 3, 4) })
  on('session.receive', (_$, e) => ({ text: e.text }))
  // 配送の底: deliver=false で「宛先が無い」失敗を演じる (底は最初の $ の呼び出しより前に登録する必要がある)
  on('session.send', () => (deliver ? { isDelivered: true as const } : { isDelivered: false as const, reason: 'nobody by that name' }))
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

test('受信と送信が帯に出て、見出しに回数が出る', async ($, on) => {
  floor(on)
  await $.session.receive({ origin: { kind: 'peer' }, text: 'issue 803 は私が持っています\n進めてよいですか' })
  await $.session.send({ to: 'obaket-2', text: 'どうぞ', origin: { kind: 'model' } as never })

  const ui = await $.ui.mount({ plugin: 'peer-inbox', surface: 'terminal', ...BAND })
  expect(await ui.find({ type: 'Text', text: /他セッション 受信 1・送信 1$/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /issue 803 は私が持っています 進めてよいですか/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /⇨ obaket-2/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /未読|既読/ })).toBeUndefined() // 既読・未読は持たない
  await ui.unmount()
})

test('status line に受信・送信の回数を常に出す (返信しても消えない)', async ($, on) => {
  floor(on)
  const status: Array<string | undefined> = []
  on('ui.status', (_$, e) => {
    status.push(e.text)
    return { value: undefined }
  })
  await $.session.receive({ origin: { kind: 'peer' }, text: 'push を控えて' })
  expect(status.at(-1)).toBe('📨 受信 1・送信 0')
  await $.session.receive({ origin: { kind: 'peer' }, text: '解除しました' })
  await $.session.send({ to: 'macos-a1', text: '了解', origin: { kind: 'model' } as never })
  expect(status.at(-1)).toBe('📨 受信 2・送信 1')
})

test('届かなかった送信は回数に数えない (帯には残す)', async ($, on) => {
  floor(on, false)
  const status: Array<string | undefined> = []
  on('ui.status', (_$, e) => {
    status.push(e.text)
    return { value: undefined }
  })
  await $.session.receive({ origin: { kind: 'peer' }, text: 'ping' })
  await $.session.send({ to: 'ghost', text: '返信', origin: { kind: 'model' } as never })
  expect(status.at(-1)).toBe('📨 受信 1・送信 0')
  const ui = await $.ui.mount({ plugin: 'peer-inbox', surface: 'terminal', ...BAND })
  expect(await ui.find({ type: 'Text', text: /⇨ ghost/ })).toBeDefined()
  await ui.unmount()
})

test('回数は帯の履歴の上限 (50 件) を超えても数え続ける', async ($, on) => {
  floor(on)
  for (let n = 0; n < 55; n++) {
    await $.session.receive({ origin: { kind: 'peer' }, text: `m${n}` })
  }
  const ui = await $.ui.mount({ plugin: 'peer-inbox', surface: 'terminal', ...BAND })
  expect(await ui.find({ type: 'Text', text: /受信 55・送信 0$/ })).toBeDefined()
  await ui.unmount()
})

test('読み直し (session.start) の後も、数えた回数を status line に出し直す', async ($, on) => {
  floor(on)
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  const status: Array<string | undefined> = []
  on('ui.status', (_$, e) => {
    status.push(e.text)
    return { value: undefined }
  })
  await $.session.receive({ origin: { kind: 'peer' }, text: 'one' })
  status.length = 0 // 受信で出した分を捨て、session.start が出し直したかだけを見る
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true } as never)
  expect(status).toEqual(['📨 受信 1・送信 0'])
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
  expect(await ui.find({ type: 'Text', text: /他セッション 受信 2・送信 0/ })).toBeDefined()
  await ui.unmount()
})
