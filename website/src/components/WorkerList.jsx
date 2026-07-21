import React, { useCallback, useState } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import { useDispatch } from 'react-redux'
import setWorkerPaused from '../actions/setWorkerPaused.js'
import deleteWorker from '../actions/deleteWorker.js'
import Button from './Button.js'
import RagIndicator from './RagIndicator.js'
import TextSpan, { TextDiv } from './TextSpan.js'
import ObjectCapsules from './ObjectCapsules.jsx'
import { formatRelative } from '../utils/time.js'
import { LabelWorker } from '../utils/labels.js'

const List = styled.div`
  display: flex;
  flex-direction: column;
`

const Row = styled.div`
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 0.75rem;
  padding: 0.625rem 0.25rem;
  border-bottom: 1px solid ${(props) => props.theme.color.neutral[props.theme.dark ? 800 : 200]};

  &:last-of-type {
    border-bottom: none;
  }
`

const Identity = styled.div`
  flex-grow: 1;
  min-width: 0;
`

const Platform = styled.div`
  min-width: 9rem;
`

const Actions = styled.div`
  display: flex;
  flex-direction: row;
  gap: 0.5rem;
`

const isPaused = (worker) => Boolean(worker.status?.paused)

const platformOf = (worker) => {
  const labels = worker.metadata?.labels || {}
  const os = labels[LabelWorker.OS]
  const arch = labels[LabelWorker.Arch]

  return os && arch ? `${os}/${arch}` : os || arch || '—'
}

const WorkerRow = ({ worker, runnerName }) => {
  const dispatch = useDispatch()
  const [busy, setBusy] = useState(false)
  const paused = isPaused(worker)
  const name = worker.metadata.name

  const run = useCallback(
    async (action) => {
      setBusy(true)
      try {
        await dispatch(action)
      } finally {
        setBusy(false)
      }
    },
    [dispatch]
  )

  const togglePause = useCallback(
    () => run(setWorkerPaused(name, !paused, runnerName)),
    [name, paused, runnerName, run]
  )

  // Dropping a worker only revokes this registration. The process keeps its
  // runner token and can register again, so the confirmation says so rather than
  // implying the worker has been barred.
  const drop = useCallback(() => {
    if (
      confirm(
        `Drop worker "${name}"?\n\nThis revokes its current registration. It can register again ` +
          `unless the runner is disabled.`
      )
    ) {
      run(deleteWorker(worker, runnerName))
    }
  }, [worker, name, runnerName, run])

  return (
    <Row>
      <RagIndicator color={paused ? 'warning' : 'success'} />
      <Identity>
        <TextDiv size="small" level={2} weight={500}>
          {name}
        </TextDiv>
        <TextDiv size="small" level={4}>
          registered {formatRelative(worker.metadata.creationTimestamp)}
        </TextDiv>
      </Identity>
      <Platform>
        <TextSpan size="small" level={3}>
          {platformOf(worker)}
        </TextSpan>
      </Platform>
      <div>
        <TextSpan size="small" weight={500} level={2} color={paused ? 'warning' : 'success'}>
          {paused ? 'paused' : 'taking jobs'}
        </TextSpan>
      </div>
      <Actions>
        <Button size="small" color="neutral" onClick={togglePause} disabled={busy}>
          {paused ? 'Resume' : 'Pause'}
        </Button>
        <Button size="small" color="error" onClick={drop} disabled={busy}>
          Drop
        </Button>
      </Actions>
    </Row>
  )
}

WorkerRow.propTypes = {
  worker: PropTypes.object.isRequired,
  runnerName: PropTypes.string.isRequired,
}

const WorkerList = ({ workers, runnerName, ...rest }) => (
  <List {...rest}>
    {workers.map((worker) => (
      <WorkerRow key={worker.metadata.uid} worker={worker} runnerName={runnerName} />
    ))}
  </List>
)

WorkerList.propTypes = {
  workers: PropTypes.arrayOf(PropTypes.object).isRequired,
  runnerName: PropTypes.string.isRequired,
}

export default WorkerList
