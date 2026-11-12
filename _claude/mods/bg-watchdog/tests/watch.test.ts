import { expect, test } from 'claude-code/testing'

import { EMPTY, alertText, due, finishedIds, launched, wakePrompt } from '../hooks/watch'
import type { Watch } from '../types'

// 2.1.322 の本物の通知 (probe の mod で prompt.submit の e.text を写した。パスだけ短くした)
const REAL_NOTIFICATION =
  '<task-notification>\n<task-id>bjz027mo4</task-id>\n<tool-use-id>toolu_01DRyATbxJg2Gh5EbT9i2HGG</tool-use-id>\n' +
  '<output-file>/private/tmp/claude-501/x/tasks/bjz027mo4.output</output-file>\n<status>completed</status>\n' +
  '<summary>Background command "Sleep 90 seconds in background" completed (exit code 0)</summary>\n</task-notification>'

test('通知から終わったタスクの id を読む (本物の形)', () => {
  expect(finishedIds(REAL_NOTIFICATION)).toEqual(['bjz027mo4'])
})

test('通知が複数並べば全部、終わっていない状態 (status 無し・途中) は読まない', () => {
  const failed = REAL_NOTIFICATION.replace('bjz027mo4', 'b2').replace('completed', 'failed')
  const progress = '<task-notification>\n<task-id>m1</task-id>\n<status>running</status>\n</task-notification>'
  const noStatus = '<task-notification>\n<task-id>m2</task-id>\n<summary>event</summary>\n</task-notification>'
  expect(finishedIds(`${REAL_NOTIFICATION}\n${failed}\n${progress}\n${noStatus}`)).toEqual(['bjz027mo4', 'b2'])
  expect(finishedIds('ふつうのプロンプト <task-id>x</task-id>')).toEqual([])
})

test('起動の読み取り: Bash の backgroundTaskId と Agent の async_launched (本物の形)', () => {
  const bash = launched('Bash', { command: 'sleep 90', description: 'Sleep 90 seconds' }, { stdout: '', stderr: '', interrupted: false, backgroundTaskId: 'bjz027mo4' }, 5)
  expect(bash).toEqual({ id: 'bjz027mo4', kind: 'shell', label: 'Bash: Sleep 90 seconds', startedAt: 5 })
  const agent = launched('Agent', { description: 'Reply DONE probe', prompt: 'p' }, { isAsync: true, status: 'async_launched', agentId: 'aa197812d27cbf0a6' }, 7)
  expect(agent).toEqual({ id: 'aa197812d27cbf0a6', kind: 'agent', label: 'Agent: Reply DONE probe', startedAt: 7 })
})

test('背景で走らないものは拾わない (前景の Bash・前景の Agent・タスク一覧の TaskCreate)', () => {
  expect(launched('Bash', { command: 'ls' }, { stdout: 'a', stderr: '', interrupted: false }, 1)).toBeNull()
  expect(launched('Agent', { prompt: 'p' }, { status: 'completed', agentId: 'a1' }, 1)).toBeNull()
  expect(launched('TaskCreate', { subject: 's' }, { taskId: '3' }, 1)).toBeNull()
  expect(launched('Bash', { command: 'x' }, 'not an object', 1)).toBeNull()
})

const waiting = (over: Partial<Watch> = {}): Watch => ({
  ...EMPTY,
  tasks: { b1: { id: 'b1', kind: 'shell', label: 'Bash: make test', startedAt: 0 } },
  lastActivityAt: 0,
  ...over,
})
const MIN = 60_000

test('知らせる時: 閾値ちょうどから。タスク無し・ターン中・閾値未満では知らせない', () => {
  expect(due(waiting(), 20 * MIN, 20 * MIN)).toBe(true)
  expect(due(waiting(), 20 * MIN - 1, 20 * MIN)).toBe(false)
  expect(due(waiting({ tasks: {} }), 99 * MIN, 20 * MIN)).toBe(false)
  expect(due(waiting({ busy: true }), 99 * MIN, 20 * MIN)).toBe(false)
})

test('知らせた後は、同じ閾値が過ぎるまで繰り返さない', () => {
  expect(due(waiting({ alertedAt: 20 * MIN }), 39 * MIN, 20 * MIN)).toBe(false)
  expect(due(waiting({ alertedAt: 20 * MIN }), 40 * MIN, 20 * MIN)).toBe(true)
})

test('文面: 動きの無い分数と、起動の古い順にタスクを 2 件まで並べ、残りは件数', () => {
  const w = waiting({
    tasks: {
      b2: { id: 'b2', kind: 'shell', label: 'Bash: b', startedAt: 10 * MIN },
      b1: { id: 'b1', kind: 'shell', label: 'Bash: a', startedAt: 0 },
      a1: { id: 'a1', kind: 'agent', label: 'Agent: c', startedAt: 15 * MIN },
    },
    lastActivityAt: 16 * MIN,
  })
  expect(alertText(w, 36 * MIN)).toBe('🚨 背景タスクの完了待ちのまま 20 分動きがない: Bash: a (起動から 36 分)、Bash: b (起動から 26 分) ほか 1 件')
  expect(wakePrompt(w, 36 * MIN)).toContain('- agent a1: Agent: c (起動から 21 分)')
})
