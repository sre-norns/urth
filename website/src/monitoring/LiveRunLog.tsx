import {useEffect, useRef, useState} from 'react'
import {Button, ErrorState, LoadingState, Status} from '@sre-norns/components'
import {useAccountId, useIdentity, useSessionEpoch} from '@sre-norns/components/identity'
import {ApiProblem} from '../api/http'
import {streamLogs} from '../api/logs'
import {resourcePath} from '../api/queries'

export function LiveRunLog({project, scenario, run, running, terminal}: {project: string; scenario: string; run: string; running: boolean; terminal: boolean}) {
  const {session} = useIdentity()
  const account = useAccountId()
  const epoch = useSessionEpoch()
  const [lines, setLines] = useState<string[]>([])
  const [state, setState] = useState('connecting')
  const [error, setError] = useState<unknown>()
  const [gap, setGap] = useState(false)
  const [retry, setRetry] = useState(0)
  const surface = useRef<HTMLPreElement>(null)
  const pinned = useRef(true)
  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    const unsubscribe = session.subscribe(() => {if (session.epoch() !== epoch || session.accountId() !== account) controller.abort()})
    setLines([]); setError(undefined); setGap(false); setState('connecting')
    async function connect(attempt: number) {
      let complete = false
      let received: string[] = []
      try {
        // NATS tails only new messages. Keep earlier lines across reconnects;
        // a terminal response replaces them with the complete stored artifact.
        await streamLogs(session, `${resourcePath('projects', project, 'scenarios', scenario)}/results/${encodeURIComponent(run)}/logs`, controller.signal, (frame) => {
          if (controller.signal.aborted) return
          if (frame.event === 'end') {complete = frame.data !== 'stream deadline reached'; if (complete) {setLines(received); setState('complete')}}
          if (frame.event === 'message') {received = received.concat(frame.data.split('\n')).slice(-5000); setState('live'); setLines((old) => old.concat(frame.data.split('\n')).slice(-5000))}
        }, () => {if (!controller.signal.aborted) setState('live')})
        if (!complete && !controller.signal.aborted) throw new Error('The log stream disconnected.')
      } catch (error) {
        if (controller.signal.aborted) return
        const retryable = !(error instanceof ApiProblem) || [409, 404, 429, 500, 502, 503, 504].includes(error.status)
        if ((running || (error instanceof ApiProblem && error.status === 404)) && attempt < 3 && retryable) {
          setState('reconnecting'); setGap(true); timer = setTimeout(() => void connect(attempt + 1), 1000 * 2 ** attempt)
        } else {setError(error); setState('error')}
      }
    }
    void connect(0)
    return () => {controller.abort(); unsubscribe(); if (timer) clearTimeout(timer)}
  }, [session, account, epoch, project, scenario, run, running, retry])
  useEffect(() => {if (surface.current && pinned.current) surface.current.scrollTop = surface.current.scrollHeight}, [lines])
  return <>
    {state === 'connecting' && <LoadingState />}
    {/* A finished run's log is stored, not live: there is nothing to reconnect to.
        When a run finishes while open, `running` changes and the effect fetches the stored log. */}
    <div className="actions"><Status value={state} />{!terminal && <Button variant="secondary" onClick={() => setRetry((r) => r + 1)}>Reconnect log</Button>}</div>
    {Boolean(error) && <ErrorState error={error} retry={terminal ? () => setRetry((r) => r + 1) : undefined} />}
    {gap && state !== 'complete' && <p>Some live output may be missing after a reconnect. The stored log replaces it when the run finishes.</p>}
    <pre className="run-log" ref={surface} aria-label="Run log" tabIndex={0} onScroll={(event) => {const el = event.currentTarget; pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 32}}>{lines.length ? lines.join('\n') : 'No log lines received.'}</pre>
  </>
}
