import type {SessionClient} from '@sre-norns/components/identity'
import {productProblem} from './http'

export type LogEvent = {event: string; data: string}
/** Incremental SSE decoding, including multiline data and split CRLF/UTF-8 chunks. */
export async function readEvents(response: Response, onEvent: (event: LogEvent) => void, signal: AbortSignal) {
  if (!response.body) throw new Error('The browser did not provide a log stream.')
  if (!response.headers.get('Content-Type')?.startsWith('text/event-stream')) throw new Error('The service returned an unsupported log stream.')
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = '', event = '', data: string[] = []
  const cancel = () => {void reader.cancel().catch(() => {})}
  signal.addEventListener('abort', cancel, {once: true})
  function line(value: string) {
    if (!value) {
      if (data.length) onEvent({event: event || 'message', data: data.join('\n')})
      data = []; event = ''; return
    }
    const colon = value.indexOf(':')
    const field = colon < 0 ? value : value.slice(0, colon)
    const text = colon < 0 ? '' : value.slice(colon + 1).replace(/^ /, '')
    if (field === 'event') event = text
    if (field === 'data') data.push(text)
  }
  try {
    while (!signal.aborted) {
      const chunk = await reader.read()
      buffer += decoder.decode(chunk.value, {stream: !chunk.done})
      let index: number
      while ((index = buffer.indexOf('\n')) >= 0) {
        line(buffer.slice(0, index).replace(/\r$/, '')); buffer = buffer.slice(index + 1)
      }
      if (chunk.done) break
      // Protect against a malformed stream that never terminates a frame.
      if (buffer.length + data.reduce((sum, value) => sum + value.length, 0) > 8 * 1024 * 1024) throw new Error('The log frame exceeds the display limit.')
    }
  } finally {signal.removeEventListener('abort', cancel); await reader.cancel().catch(() => {}); reader.releaseLock()}
}
export async function streamLogs(session: SessionClient, path: string, signal: AbortSignal, onEvent: (event: LogEvent) => void, onOpen?: () => void) {
  const response = await session.authenticatedFetch(path, {headers: {Accept: 'text/event-stream'}, signal})
  if (!response.ok) throw productProblem(response.status, await response.json().catch(() => null))
  onOpen?.()
  await readEvents(response, onEvent, signal)
}
