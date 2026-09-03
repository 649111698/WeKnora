import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./useChatStreamHandler.ts', import.meta.url), 'utf8')

test('failed tool results keep stdout/output instead of replacing it with the short error', () => {
  assert.match(source, /toolCallEvent\.output = dataPayload\.output \|\| data\.content/)
  assert.doesNotMatch(
    source,
    /toolCallEvent\.output = success\s*\?[\s\S]*dataPayload\.error/,
  )
})

test('answer events with supersede_prior retract earlier streamed answer segments', () => {
  // The flag handler must run before the incoming event is appended and
  // must reuse the same retraction as the tool_call path (superseded + done).
  const answerStart = source.indexOf("case 'answer':")
  const answerCase = source.slice(answerStart, source.indexOf("case '", answerStart + 10))
  assert.match(answerCase, /dataPayload\?\.supersede_prior/, 'answer case must check the flag')
  assert.match(
    answerCase,
    /ev\.superseded = true\s*\n\s*ev\.done = true/,
    'retraction must mark events superseded and done',
  )
  assert.match(
    answerCase,
    /message\.content = recomposeAgentAnswer\(message\)/,
    'message content must be recomposed after retraction',
  )
  // recompose must keep skipping superseded segments, so retracted preambles
  // never render into the final answer.
  assert.match(source, /if \(e\.type === 'answer' && !e\.superseded && e\.content\)/)
})

test('later tool_call events merge arguments onto the same pending card', () => {
  assert.match(source, /function mergeToolCallArguments/)
  assert.match(source, /toolCallEvent\.arguments = mergeToolCallArguments\(toolCallEvent\.arguments, incomingArguments\)/)
})
