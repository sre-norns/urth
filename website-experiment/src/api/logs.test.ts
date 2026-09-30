import {expect, it} from 'vitest'
import {readEvents, type LogEvent} from './logs'

it('decodes arbitrary UTF-8 chunk boundaries, comments, CRLF and multiline events', async () => {
  const bytes = new TextEncoder().encode(': keepalive\r\ndata: café\r\ndata: second line\r\n\r\nevent: end\ndata: completed\n\n')
  const stream = new ReadableStream<Uint8Array>({start(controller) {for (const byte of bytes) controller.enqueue(new Uint8Array([byte])); controller.close()}})
  const events: LogEvent[] = []
  await readEvents(new Response(stream, {headers: {'Content-Type': 'text/event-stream'}}), (event) => events.push(event), new AbortController().signal)
  expect(events).toEqual([{event: 'message', data: 'café\nsecond line'}, {event: 'end', data: 'completed'}])
})
it('cancels a blocked reader when the scope is abandoned', async () => {
  let cancelled = false
  const controller = new AbortController()
  const response = new Response(new ReadableStream({cancel() {cancelled = true}}), {headers: {'Content-Type': 'text/event-stream'}})
  const reading = readEvents(response, () => {}, controller.signal)
  controller.abort()
  await reading
  expect(cancelled).toBe(true)
})
