import React, { useEffect, useRef, useState } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import Panel from './Panel.js'
import TextSpan from './TextSpan.js'

const LogSurface = styled.pre`
  margin: 0;
  padding: 0.75rem;
  max-height: 24rem;
  overflow: auto;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8125rem;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
  background: rgba(0, 0, 0, 0.03);
  border-radius: 4px;
`

const SectionHeader = styled.div`
  display: flex;
  flex-direction: row;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 0.75rem;
`

// A run's log is streamed while the run is going and served from the stored
// artifact once it has finished -- the same URL either way, so this component
// does not have to know which it is getting.
//
// The line cap is not cosmetic: a chatty probe can emit output faster than a
// browser can lay it out, and an unbounded array here is a tab that eventually
// stops responding.
const MAX_LINES = 5000

const LiveRunLog = ({ runId, scenarioName, isRunning }) => {
  const [lines, setLines] = useState([])
  const [error, setError] = useState(null)
  const [ended, setEnded] = useState(false)
  const surfaceRef = useRef(null)
  const pinnedToBottom = useRef(true)

  useEffect(() => {
    if (!scenarioName || !runId) {
      return undefined
    }

    setLines([])
    setError(null)
    setEnded(false)

    // Absent in jsdom, and in any browser old enough to lack SSE. Degrade to an
    // empty panel rather than throwing during render: a missing log is a
    // nuisance, a component that throws takes the whole run page down with it.
    if (typeof EventSource === 'undefined') {
      setError('Live logs are not supported in this browser')
      return undefined
    }

    const source = new EventSource(
      `/api/v1/scenarios/${encodeURIComponent(scenarioName)}/results/${encodeURIComponent(runId)}/logs`
    )

    source.onmessage = (event) => {
      setLines((previous) => {
        const next = previous.concat(event.data)
        return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
      })
    }

    source.addEventListener('end', () => {
      setEnded(true)
      source.close()
    })

    source.onerror = () => {
      // EventSource reconnects on its own, and a finished run closes the
      // stream normally -- which also surfaces here. Only report a problem if
      // the connection is actually done and we never got an end event.
      if (source.readyState === EventSource.CLOSED) {
        setError('Log stream disconnected')
      }
    }

    return () => source.close()
  }, [runId, scenarioName])

  // Follow the tail, but stop following the moment the reader scrolls up --
  // yanking someone back to the bottom while they are reading is worse than
  // not auto-scrolling at all.
  useEffect(() => {
    const surface = surfaceRef.current
    if (surface && pinnedToBottom.current) {
      surface.scrollTop = surface.scrollHeight
    }
  }, [lines])

  const onScroll = () => {
    const surface = surfaceRef.current
    if (!surface) {
      return
    }

    const distanceFromBottom = surface.scrollHeight - surface.scrollTop - surface.clientHeight
    pinnedToBottom.current = distanceFromBottom < 32
  }

  return (
    <Panel>
      <SectionHeader>
        <TextSpan size="large" weight={500} level={2}>
          Run log
        </TextSpan>
        <TextSpan size="small" level={4}>
          {error || (isRunning && !ended ? 'streaming…' : `${lines.length} lines`)}
        </TextSpan>
      </SectionHeader>

      <LogSurface ref={surfaceRef} onScroll={onScroll} data-testid="run-log-surface">
        {lines.join('\n')}
      </LogSurface>
    </Panel>
  )
}

LiveRunLog.propTypes = {
  runId: PropTypes.string.isRequired,
  // The log endpoint is scoped to a scenario, so the panel cannot be rendered
  // until the run's scenario is known.
  scenarioName: PropTypes.string,
  isRunning: PropTypes.bool,
}

export default LiveRunLog
