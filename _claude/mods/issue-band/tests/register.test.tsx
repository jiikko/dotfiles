import type { On } from 'claude-code'
import { test, expect, mock } from 'claude-code/testing'

const PROPS = { hasSurvey: false, isWorking: false, maxRows: 10, bodyColumns: 120, title: '', isFocused: false }
const NONE = { human: 'overdue=0\nsoon=0\nhuman=3\nbroken=0\n', retro: 'retro=0\nheld=0\nodd=0\n' }
const LATE = { human: 'overdue=1\nsoon=0\nhuman=3\nbroken=0\n', retro: 'retro=2\nheld=0\nodd=0\n' }

type Out = { human: string; retro: string; exitCode?: number }
type Run = { argv: string[]; cwd?: string; stdin?: string }

// test の on は engine の役 (plugin の下に座る)。script の --counts を答える。out は呼ばれるたびに読むので、途中で差し替えられる
const engineBeneath = (on: On, runs: Run[], out: { now: Out }, surfaces: string[] = ['terminal'], entrypoint?: string, gate?: Promise<void>) => {
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('turn.complete', () => ({ text: '' }))
  on('session.cwd', () => ({ value: '/w' }))
  on('session.surfaces', () => ({ value: surfaces }))
  // engine の帯: このテストでは空の Box だけを描く (要素を返す決まり)
  on('ui.render', ($, e) => {
    const { Box } = $.ui.resolve(e)
    return <Box key="engine-band" />
  })
  on('process.run', async ($, e) => {
    await gate
    runs.push({ argv: [...e.argv], cwd: e.init?.cwd, stdin: typeof e.init?.stdin === 'string' ? e.init.stdin : undefined })
    const which = e.argv[0]?.endsWith('/human-tasks-due.sh') ? 'human' : 'retro'
    // $ の口を test が答えるときは { value } で包む
    return { value: { exitCode: out.now.exitCode ?? 0, stdout: out.now[which], stderr: out.now.exitCode ? 'boom' : '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  mock.env(on, entrypoint ? { CLAUDE_CODE_ENTRYPOINT: entrypoint } : {})
}

const TURN = { reason: 'answer', answer: 'ok', durationMs: 1, isAborted: false, turnId: 't1' } as const

test('対話のセッションでは、mod の 2 段上の hooks/ の script をセッションの cwd で --counts 付きで呼ぶ', async ($, on) => {
  const runs: Run[] = []
  engineBeneath(on, runs, { now: NONE })
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  expect(runs.map(r => r.argv.slice(1))).toEqual([['--counts'], ['--counts']])
  expect(runs.map(r => r.argv[0]?.replace(/^.*\/_claude\//, ''))).toEqual(['mods/issue-band/../../hooks/human-tasks-due.sh', 'mods/issue-band/../../hooks/retro-open.sh'])
  expect(runs.map(r => [r.cwd, r.stdin])).toEqual([['/w', '{"cwd":"/w"}'], ['/w', '{"cwd":"/w"}']])
})

test('非対話のセッション (claude -p) では script を呼ばない', async ($, on) => {
  const runs: Run[] = []
  engineBeneath(on, runs, { now: NONE }, [])
  await $.session.start({ cwd: '/w', surface: null, isInteractive: false })
  // @ts-ignore: turn.complete の入力の型は省く
  await $.turn.complete(TURN)
  expect(runs).toEqual([])
})

// desktop が起こすセッションは、型定義 (SessionStartInput) では開始時に surface が null・isInteractive が false・名簿が空 (`claude -p` と同じ見た目)
test('desktop が起こしたセッションは、開始時に isInteractive=false・名簿が空でも数える (起動は待たせず裏で)', async ($, on) => {
  const clock = mock.clock(on)
  const runs: Run[] = []
  engineBeneath(on, runs, { now: NONE }, [], 'claude-desktop')
  await $.session.start({ cwd: '/w', surface: null, isInteractive: false })
  await clock.advance(1) // 裏で走らせた数え直しが落ち着くまで待つ
  expect(runs.map(r => r.argv.slice(1))).toEqual([['--counts'], ['--counts']])
})

test('desktop が起こしたセッションの数え直しは、起動を待たせない (script が終わる前に session.start が返る)', async ($, on) => {
  let release = () => {}
  const gate = new Promise<void>(resolve => { release = resolve })
  const clock = mock.clock(on)
  const runs: Run[] = []
  engineBeneath(on, runs, { now: NONE }, [], 'claude-desktop', gate)
  await $.session.start({ cwd: '/w', surface: null, isInteractive: false })
  expect(runs).toEqual([]) // script はまだ終わっていない (gate で止めてある)
  release()
  await clock.advance(1)
  expect(runs.length).toBe(2)
})

test('desktop が起こしたセッションは、メインのターンの終わりにも、名簿が空のままで数え直す', async ($, on) => {
  const clock = mock.clock(on)
  const runs: Run[] = []
  engineBeneath(on, runs, { now: NONE }, [], 'claude-desktop')
  await $.session.start({ cwd: '/w', surface: null, isInteractive: false })
  // @ts-ignore: turn.complete の入力の型は省く
  await $.turn.complete(TURN)
  await clock.advance(1)
  expect(runs.length).toBe(4)
})

for (const surface of ['terminal', 'desktop'] as const) {
  test(`${surface}: 期限切れと retro があれば帯に出す`, async ($, on) => {
    engineBeneath(on, [], { now: LATE })
    await $.session.start({ cwd: '/w', surface, isInteractive: true })
    // @ts-ignore: AbovePrompt の props の一部 (scroll / view) は帯の描画に使わないので省く
    const ui = await $.ui.mount({ plugin: 'issue-band', surface, component: 'AbovePrompt', props: PROPS })
    expect(await ui.find({ type: 'Text', text: '期限切れの human 1 件' })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: 'retro 未決着 2' })).toBeDefined()
    await ui.unmount()
  })

  test(`${surface}: 余裕のある human だけなら帯を出さない`, async ($, on) => {
    engineBeneath(on, [], { now: NONE })
    await $.session.start({ cwd: '/w', surface, isInteractive: true })
    // @ts-ignore: 上と同じ
    const ui = await $.ui.mount({ plugin: 'issue-band', surface, component: 'AbovePrompt', props: PROPS })
    // mod は自分の帯を描かず engine に任せる (空の帯で 1 行を取らない)
    expect(await ui.find({ key: 'engine-band' })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /human/ })).toBeUndefined()
    await ui.unmount()
  })
}

test('survey が帯を使っている間は譲る', async ($, on) => {
  engineBeneath(on, [], { now: LATE })
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: 上と同じ
  const ui = await $.ui.mount({ plugin: 'issue-band', surface: 'terminal', component: 'AbovePrompt', props: { ...PROPS, hasSurvey: true } })
  expect(await ui.find({ key: 'engine-band' })).toBeDefined()
  await ui.unmount()
})

test('script が非 0 で終わったら、黙って消さずに理由を帯に出す', async ($, on) => {
  engineBeneath(on, [], { now: { human: '', retro: '', exitCode: 1 } })
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: 上と同じ
  const ui = await $.ui.mount({ plugin: 'issue-band', surface: 'terminal', component: 'AbovePrompt', props: PROPS })
  expect(await ui.find({ type: 'Text', text: /issue の催促を数えられない: .*rc=1 boom/ })).toBeDefined()
  await ui.unmount()
})

test('メインのターンの終わりに数え直す (issue を done へ移した後に帯が消える)', async ($, on) => {
  const clock = mock.clock(on)
  const out = { now: LATE }
  engineBeneath(on, [], out)
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  out.now = NONE
  // @ts-ignore: turn.complete の入力の型は省く
  await $.turn.complete(TURN)
  await clock.advance(1) // 裏で走らせた数え直しが落ち着くまで待つ
  // @ts-ignore: 上と同じ
  const ui = await $.ui.mount({ plugin: 'issue-band', surface: 'terminal', component: 'AbovePrompt', props: PROPS })
  expect(await ui.find({ type: 'Text', text: /期限切れ/ })).toBeUndefined()
  await ui.unmount()
})

test('サブエージェントのターン (agentId あり) では数え直さない', async ($, on) => {
  const clock = mock.clock(on)
  const runs: Run[] = []
  engineBeneath(on, runs, { now: NONE })
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: 上と同じ
  await $.turn.complete({ ...TURN, agentId: 'a1' })
  await clock.advance(1)
  expect(runs.length).toBe(2) // session.start の 2 本だけ
})
