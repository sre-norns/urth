import {useEffect, useRef, useState} from 'react'
import {api} from '../api'
import {ErrorState, Loading, Status} from './ui'

const MAX_LINES = 5_000

function storedLines(payload: string): string[] {
  // The real endpoint uses SSE for stored and live logs. Accept plain text too
  // because reverse proxies and test fixtures may have already decoded it.
  if (!payload.includes('data:')) return payload.split(/\r?\n/)
  return payload
    .split(/\r?\n/)
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.replace(/^data:\s?/, ''))
}

export function LiveRunLog({
  scenarioName,
  runName,
  running,
}: {
  scenarioName: string
  runName: string
  running: boolean
}) {
  const [lines, setLines] = useState<string[]>([])
  const [state, setState] = useState<'connecting' | 'live' | 'complete' | 'error'>('connecting')
  const [error, setError] = useState<unknown>()
  const surface = useRef<HTMLPreElement>(null)
  const pinned = useRef(true)

  useEffect(() => {
    setLines([])
    setError(undefined)
    setState('connecting')

    if (typeof EventSource === 'undefined') {
      if (running) {
        setState('error')
        setError(new Error('Live logs are not supported in this browser'))
        return
      }
      const controller = new AbortController()
      api.runs.logs(scenarioName, runName, controller.signal)
        .then((payload) => {
          setLines(storedLines(payload))
          setState('complete')
        })
        .catch((nextError) => {
          if (!controller.signal.aborted) {
            setError(nextError)
            setState('error')
          }
        })
      return () => controller.abort()
    }

    const source = new EventSource(
      `/api/v1/scenarios/${encodeURIComponent(scenarioName)}/results/${encodeURIComponent(runName)}/logs`,
    )
    source.onopen = () => setState('live')
    source.onmessage = (event) => {
      setState('live')
      setLines((previous) => {
        const next = previous.concat(event.data)
        return next.length > MAX_LINES ? next.slice(-MAX_LINES) : next
      })
    }
    source.addEventListener('end', () => {
      setState('complete')
      source.close()
    })
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) {
        setError(new Error('Log stream disconnected'))
        setState('error')
      }
    }
    return () => source.close()
  }, [runName, running, scenarioName])

  useEffect(() => {
    if (surface.current && pinned.current) surface.current.scrollTop = surface.current.scrollHeight
  }, [lines])

  if (state === 'connecting') return <Loading label="Connecting to log" />
  if (state === 'error') return <ErrorState title="Log unavailable" error={error} />
  return (
    <>
      <div className="log-state">
        {state === 'live' ? <Status value="live" /> : <span>{lines.length} lines</span>}
      </div>
      <pre
        className="log-view"
        ref={surface}
        onScroll={(event) => {
          const target = event.currentTarget
          pinned.current = target.scrollHeight - target.scrollTop - target.clientHeight < 32
        }}
      >
        {lines.length ? lines.join('\n') : 'No log lines were recorded.'}
      </pre>
    </>
  )
}
