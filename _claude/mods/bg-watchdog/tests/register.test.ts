import { expect, mock, test } from 'claude-code/testing'
import type { Engine, MockClock } from 'claude-code/testing'
import type { On } from 'claude-code'

const MIN = 60_000
const TURN = { reason: 'answer', answer: 'ok', durationMs: 1, isAborted: false, turnId: 't1' } as const

type Floor = { clock: MockClock; toasts: string[]; status: Array<string | undefined>; runs: string[][]; submits: string[]; agents: Array<{ id: string; status: string }> | null; stopFails: string | null }

// テストの on は engine の役 (plugin の下に座る)。$ の口は { value } で包んで答える
const floor = (on: On): Floor => {
  const f: Floor = { clock: mock.clock(on, { now: 1_000_000 }), toasts: [], status: [], runs: [], submits: [], agents: [{ id: 'a1', status: 'running' }], stopFails: null }
  on('agent.list', () => (f.agents === null ? { deny: 'unavailable' } : { value: f.agents.map(a => ({ ...a, description: 'review', type: 'general-purpose' })) }))
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('turn.start', (_$, e) => ({ turnId: e.turnId }))
  on('turn.complete', () => ({ text: '' }))
  on('ui.toast', (_$, e) => {
    f.toasts.push(e.text)
    return { value: undefined }
  })
  on('ui.status', (_$, e) => {
    f.status.push(e.text)
    return { value: undefined }
  })
  on('process.run', (_$, e) => {
    f.runs.push([...e.argv])
    return { value: { exitCode: 0, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  on('prompt.submit', (_$, e) => {
    f.submits.push(e.text)
    return e
  })
  // 本物の結果の形 (2.1.322 で probe の mod が写した)
  on('tool.call', (_$, e) => {
    if (e.tool === 'Bash' && (e as { run_in_background?: boolean }).run_in_background === true) {
      return { result: { stdout: '', stderr: '', interrupted: false, isImage: false, noOutputExpected: false, backgroundTaskId: 'b1' } }
    }
    if (e.tool === 'Agent') return { result: { isAsync: true, status: 'async_launched', agentId: 'a1', description: 'review' } }
    if (e.tool === 'TaskStop' && f.stopFails === 'deny') return { deny: 'blocked by a hook' }
    if (e.tool === 'TaskStop' && f.stopFails !== null) return { result: f.stopFails, isError: true }
    return { result: { stdout: 'ok', stderr: '', interrupted: false } }
  })
  return f
}

// メインのターンの中で背景の Bash を起こし、ターンを終える (= 背景の完了を待って止まった状態)
async function launchAndIdle($: Engine): Promise<void> {
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: turn.start の入力の型は省く
  await $.turn.start({ turnId: 't1' })
  await $.tool.call({ tool: 'Bash', command: 'make test', description: 'make test', run_in_background: true })
  // @ts-ignore: turn.complete の入力の型は省く
  await $.turn.complete(TURN)
}

const notification = (id: string) =>
  `<task-notification>\n<task-id>${id}</task-id>\n<status>completed</status>\n<summary>done</summary>\n</task-notification>`

test('背景の完了待ちのまま 20 分動きがなければ、toast・status・tmux の script で 1 回知らせる (19 分では知らせない)', async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(19 * MIN)
  expect(f.toasts).toEqual([])
  await f.clock.advance(1 * MIN)
  expect(f.toasts.length).toBe(1)
  expect(f.toasts[0]).toContain('背景タスクの完了待ちのまま 20 分動きがない: Bash: make test')
  expect(f.status.at(-1)).toBe('🚨 背景待ち 20 分')
  expect(f.runs.length).toBe(1)
  expect(f.runs[0]?.[0]?.replace(/^.*\/_claude\//, '')).toBe('mods/bg-watchdog/../../hooks/tmux-pane-state.sh')
  expect(f.runs[0]?.slice(1, 2)).toEqual(['bg-stuck'])
  expect(f.submits).toEqual([]) // wake を指定しないときは agent を起こさない
})

test('知らせた後も動きがなければ、さらに 20 分ごとに繰り返す', { timeoutMs: 30_000 }, async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(39 * MIN)
  expect(f.toasts.length).toBe(1)
  await f.clock.advance(1 * MIN)
  expect(f.toasts.length).toBe(2)
})

test('完了の通知が届いたタスクは見張りから外れ、知らせない', async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(5 * MIN)
  await $.prompt.submit({ text: notification('b1'), origin: { kind: 'task-notification' } })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  await f.clock.advance(60 * MIN)
  expect(f.toasts).toEqual([])
})

test('通知と同じ形の文でも、人が打ったプロンプトではタスクを外さない', async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await $.prompt.submit({ text: notification('b1'), origin: { kind: 'composer' } })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  await f.clock.advance(20 * MIN)
  expect(f.toasts.length).toBe(1)
})

test('動き (プロンプト・tool 呼び出し) があれば数え直し、出していた status を消す', { timeoutMs: 30_000 }, async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(20 * MIN)
  expect(f.toasts.length).toBe(1)
  await $.tool.call({ tool: 'Bash', command: 'tail out' })
  expect(f.status.at(-1)).toBeUndefined()
  const shown = f.status.length
  await $.tool.call({ tool: 'Bash', command: 'tail out' })
  expect(f.status.length).toBe(shown) // 消すのは知らせを出していた後の最初の動きだけ (tool 呼び出しのたびに status を書かない)
  await f.clock.advance(19 * MIN)
  expect(f.toasts.length).toBe(1)
  await f.clock.advance(1 * MIN)
  expect(f.toasts.length).toBe(2)
})

test('メインのターンが走っている間は知らせない (ターンの中の待ちは tool-elapsed の領分)', async ($, on) => {
  const f = floor(on)
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: 上と同じ
  await $.turn.start({ turnId: 't1' })
  await $.tool.call({ tool: 'Bash', command: 'make test', run_in_background: true })
  await f.clock.advance(30 * MIN)
  expect(f.toasts).toEqual([])
})

test('背景の subagent は、その agentId のターンの終わりで見張りから外れる', async ($, on) => {
  const f = floor(on)
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: 上と同じ
  await $.turn.start({ turnId: 't1' })
  await $.tool.call({ tool: 'Agent', prompt: 'review', description: 'review', run_in_background: true })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  // @ts-ignore: 上と同じ
  await $.turn.complete({ ...TURN, turnId: 't2', agentId: 'a1' })
  await f.clock.advance(60 * MIN)
  expect(f.toasts).toEqual([])
})

test('TaskStop で止めたタスクは見張りから外れる', async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await $.tool.call({ tool: 'TaskStop', task_id: 'b1' })
  await f.clock.advance(60 * MIN)
  expect(f.toasts).toEqual([])
})

test('wake を指定すると、知らせるときに状態を確かめさせるプロンプトを送る', { options: { wake: true } }, async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(20 * MIN)
  expect(f.submits.length).toBe(1)
  expect(f.submits[0]).toContain('[bg-watchdog]')
  expect(f.submits[0]).toContain('- shell b1: Bash: make test')
})

test('thresholdMinutes で分数を変えられる', { options: { thresholdMinutes: 5 } }, async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(5 * MIN)
  expect(f.toasts.length).toBe(1)
  expect(f.toasts[0]).toContain('5 分動きがない')
})

test('同じタスクの組には 3 回まで知らせ、人がプロンプトを打つと、また知らせる', { timeoutMs: 30_000 }, async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(100 * MIN)
  expect(f.toasts.length).toBe(3)
  expect(f.toasts[2]).toContain('この組では最後の知らせ')
  await $.prompt.submit({ text: 'どう？', origin: { kind: 'composer' } })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  await f.clock.advance(20 * MIN)
  expect(f.toasts.length).toBe(4)
})

// wake のプロンプトは本番では origin { kind: 'plugin' } で届く (2.1.322 で probe が実測)。テストの中では自分の送ったプロンプトが
// 自分の hook を通らないので、同じ origin のプロンプトを直接起こして確かめる
test('人でないプロンプト (wake・通知) では数え直さない: 知らせは 3 回で止まる', { timeoutMs: 30_000 }, async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(60 * MIN)
  expect(f.toasts.length).toBe(3)
  await $.prompt.submit({ text: '[bg-watchdog] ...', origin: { kind: 'plugin', name: 'bg-watchdog' } })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  await f.clock.advance(60 * MIN)
  expect(f.toasts.length).toBe(3)
})

test('wake を入れても、送るプロンプトは知らせと同じく 3 回で止まる', { timeoutMs: 30_000, options: { wake: true } }, async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(200 * MIN)
  expect(f.submits.length).toBe(3)
})

test('終わった (名簿で completed) 背景 agent は、通知が来なくても見張りから外れる', async ($, on) => {
  const f = floor(on)
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: 上と同じ
  await $.turn.start({ turnId: 't1' })
  await $.tool.call({ tool: 'Agent', prompt: 'review', description: 'review', run_in_background: true })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  f.agents = [{ id: 'a1', status: 'completed' }]
  await f.clock.advance(60 * MIN)
  expect(f.toasts).toEqual([])
})

test('走っている背景 agent は、名簿にいる限り見張る', async ($, on) => {
  const f = floor(on)
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: 上と同じ
  await $.turn.start({ turnId: 't1' })
  await $.tool.call({ tool: 'Agent', prompt: 'review', description: 'review', run_in_background: true })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  await f.clock.advance(20 * MIN)
  expect(f.toasts.length).toBe(1)
  expect(f.toasts[0]).toContain('Agent: review')
})

for (const [name, fails] of [['失敗した', 'The user denied this tool call'], ['{ deny } で拒否された', 'deny']] as const) {
  test(`${name} TaskStop では見張りから外さない (タスクはまだ走っているかもしれない)`, async ($, on) => {
    const f = floor(on)
    await launchAndIdle($)
    f.stopFails = fails
    await $.tool.call({ tool: 'TaskStop', task_id: 'b1' })
    await f.clock.advance(20 * MIN)
    expect(f.toasts.length).toBe(1)
  })
}

async function launchAgentAndIdle($: Engine): Promise<void> {
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: 上と同じ
  await $.turn.start({ turnId: 't1' })
  await $.tool.call({ tool: 'Agent', prompt: 'review', description: 'review', run_in_background: true })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
}

test('名簿に無い (engine が落とした) 背景 agent も見張りから外れる', async ($, on) => {
  const f = floor(on)
  await launchAgentAndIdle($)
  f.agents = []
  await f.clock.advance(60 * MIN)
  expect(f.toasts).toEqual([])
})

test('名簿が読めないときは agent を落とさない (落とすと止まった agent を黙って見逃す)', async ($, on) => {
  const f = floor(on)
  await launchAgentAndIdle($)
  f.agents = null
  await f.clock.advance(20 * MIN)
  expect(f.toasts.length).toBe(1)
})

test('上限まで知らせた後でも、背景タスクの組が変われば数え直す', { timeoutMs: 30_000 }, async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(60 * MIN)
  expect(f.toasts.length).toBe(3)
  // wake のターン (人ではない) の中で、別の背景 agent が増えた
  await $.prompt.submit({ text: '[bg-watchdog] ...', origin: { kind: 'plugin', name: 'bg-watchdog' } })
  // @ts-ignore: 上と同じ
  await $.turn.start({ turnId: 't3' })
  await $.tool.call({ tool: 'Agent', prompt: 'review', description: 'review', run_in_background: true })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  await f.clock.advance(20 * MIN)
  expect(f.toasts.length).toBe(4)
})

test('スマホ (Remote Control) から人が返事をしても数え直す', { timeoutMs: 30_000 }, async ($, on) => {
  const f = floor(on)
  await launchAndIdle($)
  await f.clock.advance(60 * MIN)
  await $.prompt.submit({ text: 'どう？', origin: { kind: 'bridge' } })
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  await f.clock.advance(20 * MIN)
  expect(f.toasts.length).toBe(4)
})
