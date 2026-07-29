import React, {useCallback, useEffect, useMemo, useState} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {useDispatch, useSelector} from 'react-redux'
import fetchWorker from '../actions/fetchWorker.js'
import fetchWorkerResults from '../actions/fetchWorkerResults.js'
import setWorkerPaused from '../actions/setWorkerPaused.js'
import Button from '../components/Button.js'
import EmptyInlay from '../components/EmptyInlay.jsx'
import ErrorInlay from '../components/ErrorInlay.jsx'
import Link from '../components/Link.js'
import ObjectCapsules from '../components/ObjectCapsules.jsx'
import Panel from '../components/Panel.js'
import RagIndicator from '../components/RagIndicator.js'
import SpinnerInlay from '../components/SpinnerInlay.jsx'
import StatTile from '../components/StatTile.jsx'
import TextSpan, {TextDiv} from '../components/TextSpan.js'
import RunResult from '../containers/RunResult.jsx'
import {LabelRunner, LabelWorker} from '../utils/labels.js'
import {conditionOf, describePresence, WorkerContact} from '../utils/presence.js'
import {RESOURCE_REFRESH_INTERVAL_MS} from '../utils/refresh.js'
import {formatRelative, formatTimestamp} from '../utils/time.js'

const PageContainer = styled.div`
  width: 100%;
  max-width: 1320px;
  margin-left: auto;
  margin-right: auto;
  padding: 1rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
`

const HeaderRow = styled.div`
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 0.75rem;

  h2 {
    margin: 0;
    flex-grow: 1;
    font-size: 1.5rem;
    font-weight: 500;
  }
`

const StatsRow = styled.div`
  display: flex;
  flex-direction: row;
  flex-wrap: wrap;
  gap: 2rem;
  margin-top: 1rem;
`

const DetailGrid = styled.div`
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: 0.5rem 1rem;
  margin-top: 1rem;
`

const Monospace = styled(TextSpan)`
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  overflow-wrap: anywhere;
`

const SectionHeader = styled.div`
  display: flex;
  flex-direction: row;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 0.75rem;
`

const SignalGrid = styled.div`
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(16rem, 1fr));
  gap: 1rem;
`

const SignalCard = styled.div`
  border: 1px solid ${(props) => props.theme.color.neutral[props.theme.dark ? 700 : 200]};
  border-radius: 0.5rem;
  padding: 0.875rem;
`

const SignalHeader = styled.div`
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
`

// RunResult uses the same OddContainer as the main Scenarios and Results lists.
// A normal Panel has the exact neutral background OddContainer gives its shaded
// rows, which makes the alternation disappear. Keep this list section on the
// page background so its odd rows read the same way as those established lists.
const RecentRunsPanel = styled(Panel)`
  background-color: transparent;
`

const SignalState = {
  online: {color: 'success', freshness: 'fresh'},
  offline: {color: 'error', freshness: 'stale'},
  unknown: {color: 'neutral', freshness: 'not observed'},
}

const signalDescription = (value) => SignalState[value] || SignalState.unknown

const platformOf = (worker) => {
  const labels = worker?.metadata?.labels || {}
  const os = labels[LabelWorker.OS]
  const arch = labels[LabelWorker.Arch]

  return os && arch ? `${os}/${arch}` : os || arch || 'unknown'
}

const contactEvidence = (via) => {
  switch (via) {
    case WorkerContact.Claim:
      return 'claim — this worker took a run'
    case WorkerContact.Heartbeat:
      return 'heartbeat'
    default:
      return 'unknown'
  }
}

const Signal = ({name, value, timestamp, evidence}) => {
  const signal = signalDescription(value)

  return (
    <SignalCard>
      <SignalHeader>
        <RagIndicator color={signal.color} />
        <TextSpan size="medium" level={2} weight={500}>
          {name}
        </TextSpan>
      </SignalHeader>
      <TextDiv size="small" level={2} weight={500} color={signal.color}>
        {value || 'unknown'} · {signal.freshness}
      </TextDiv>
      <TextDiv size="small" level={4} style={{marginTop: '0.375rem'}}>
        {timestamp ? `Last observed ${formatRelative(timestamp)}` : 'Never observed'}
      </TextDiv>
      <TextDiv size="small" level={4}>
        {formatTimestamp(timestamp)}
      </TextDiv>
      {evidence && (
        <TextDiv size="small" level={3} style={{marginTop: '0.375rem'}}>
          Evidence: {evidence}
        </TextDiv>
      )}
    </SignalCard>
  )
}

Signal.propTypes = {
  name: PropTypes.string.isRequired,
  value: PropTypes.string,
  timestamp: PropTypes.string,
  evidence: PropTypes.string,
}

const WorkerDetail = ({workerName}) => {
  const dispatch = useDispatch()
  const [busy, setBusy] = useState(false)

  const workerState = useSelector((s) => s.worker[workerName]) || {}
  const resultState = useSelector((s) => s.workerResults[workerName]) || {}

  const refresh = useCallback(() => {
    dispatch(fetchWorker(workerName))
    dispatch(fetchWorkerResults(workerName))
  }, [dispatch, workerName])

  useEffect(() => {
    refresh()

    const timer = setInterval(refresh, RESOURCE_REFRESH_INTERVAL_MS)
    return () => clearInterval(timer)
  }, [refresh])

  const worker = workerState.response
  const runs = useMemo(() => resultState.response?.data || [], [resultState.response])
  const totalRuns = Number.isFinite(resultState.response?.total) ? resultState.response.total : runs.length

  const togglePause = useCallback(async () => {
    if (!worker) {
      return
    }

    const runnerName = worker.metadata?.labels?.[LabelRunner.Name]
    const paused = Boolean(worker.status?.paused)

    setBusy(true)
    try {
      await dispatch(setWorkerPaused(workerName, !paused, runnerName))
      refresh()
    } finally {
      setBusy(false)
    }
  }, [dispatch, refresh, worker, workerName])

  if (workerState.error && !worker) {
    return <ErrorInlay message="Error loading worker" details={workerState.error.message || ''} />
  }

  if (!worker) {
    return <SpinnerInlay />
  }

  const {metadata, status = {}} = worker
  const labels = metadata.labels || {}
  const runnerName = labels[LabelRunner.Name]
  const hostname = labels[LabelWorker.Hostname] || 'unknown'
  const buildVersion = labels[LabelWorker.BuildVersion] || 'unknown'
  const paused = Boolean(status.paused)
  const presence = describePresence(conditionOf(worker))

  return (
    <PageContainer>
      <Panel>
        {runnerName && (
          <TextDiv size="small" level={3} style={{marginBottom: '0.75rem'}}>
            <Link href={`/runners/${runnerName}`}>← Runner {runnerName}</Link>
          </TextDiv>
        )}

        <HeaderRow>
          <RagIndicator color={presence.color} />
          <h2>{metadata.name}</h2>
          <Button onClick={togglePause} disabled={busy} color={paused ? 'contrast' : 'neutral'}>
            {paused ? 'Resume worker' : 'Pause worker'}
          </Button>
        </HeaderRow>

        <TextDiv size="small" level={4} style={{marginTop: '0.375rem'}}>
          {presence.detail || 'Reporting on both API and NATS paths.'}
        </TextDiv>

        <StatsRow>
          <StatTile
            caption="Availability"
            value={paused ? 'paused' : 'taking jobs'}
            color={paused ? 'warning' : 'success'}
            detail={paused ? 'registered, not claiming new work' : null}
          />
          <StatTile caption="Presence" value={presence.label} color={presence.color} />
          <StatTile caption="Jobs run" value={totalRuns} detail="all recorded executions" />
          <StatTile caption="Registered" value={formatRelative(metadata.creationTimestamp)} />
        </StatsRow>

        <DetailGrid>
          <TextSpan size="small" level={4}>
            Host
          </TextSpan>
          <TextSpan size="small" level={2}>
            {hostname}
          </TextSpan>

          <TextSpan size="small" level={4}>
            Platform
          </TextSpan>
          <TextSpan size="small" level={2}>
            {platformOf(worker)}
          </TextSpan>

          <TextSpan size="small" level={4}>
            Worker build
          </TextSpan>
          <TextSpan size="small" level={2}>
            {buildVersion}
          </TextSpan>

          <TextSpan size="small" level={4}>
            UID
          </TextSpan>
          <Monospace size="small" level={2}>
            {metadata.uid}
          </Monospace>

          <TextSpan size="small" level={4}>
            Runner
          </TextSpan>
          <TextSpan size="small" level={2}>
            {runnerName ? <Link href={`/runners/${runnerName}`}>{runnerName}</Link> : 'unknown'}
          </TextSpan>

          <TextSpan size="small" level={4}>
            Registered at
          </TextSpan>
          <TextSpan size="small" level={2}>
            {formatTimestamp(metadata.creationTimestamp)}
          </TextSpan>
        </DetailGrid>

        <TextDiv size="small" level={4} style={{marginTop: '1rem'}}>
          Advertised and server-derived labels
        </TextDiv>
        <ObjectCapsules value={labels} style={{paddingTop: '0.375rem'}} />
      </Panel>

      {workerState.error && (
        <ErrorInlay
          message="Worker refresh failed; showing the last successful reading"
          details={workerState.error.message || ''}
        />
      )}

      <Panel>
        <SectionHeader>
          <TextSpan size="large" weight={500} level={2}>
            Presence evidence
          </TextSpan>
          <TextSpan size="small" level={4}>
            Verdict: {presence.label}
          </TextSpan>
        </SectionHeader>

        <SignalGrid>
          <Signal
            name="API contact"
            value={status.presence?.api}
            timestamp={status.lastSeenTime}
            evidence={status.lastSeenTime ? contactEvidence(status.lastSeenVia) : null}
          />
          <Signal name="NATS presence" value={status.presence?.nats} timestamp={status.natsLastSeenTime} />
          <SignalCard>
            <SignalHeader>
              <RagIndicator color={status.leftAt ? 'warning' : 'neutral'} />
              <TextSpan size="medium" level={2} weight={500}>
                Departure
              </TextSpan>
            </SignalHeader>
            <TextDiv size="small" level={2} weight={500}>
              {status.leftAt ? `Announced ${formatRelative(status.leftAt)}` : 'No departure recorded'}
            </TextDiv>
            <TextDiv size="small" level={4} style={{marginTop: '0.375rem'}}>
              {formatTimestamp(status.leftAt)}
            </TextDiv>
          </SignalCard>
        </SignalGrid>
      </Panel>

      <RecentRunsPanel role="region" aria-labelledby="recent-runs-heading">
        <SectionHeader>
          <TextSpan id="recent-runs-heading" size="large" weight={500} level={2}>
            Recent runs
          </TextSpan>
          <TextSpan size="small" level={4}>
            Showing {runs.length} of {totalRuns} jobs
          </TextSpan>
        </SectionHeader>

        {resultState.fetching && !resultState.response && <SpinnerInlay />}
        {resultState.error && (
          <ErrorInlay message="Error loading this worker's runs" details={resultState.error.message || ''} />
        )}
        {!resultState.fetching && !resultState.error && runs.length === 0 && <EmptyInlay />}
        {runs.map((run, index) => (
          <RunResult key={run.uid || run.name} data={run} odd={index % 2 !== 0} />
        ))}
      </RecentRunsPanel>
    </PageContainer>
  )
}

WorkerDetail.propTypes = {
  workerName: PropTypes.string.isRequired,
}

export default WorkerDetail
