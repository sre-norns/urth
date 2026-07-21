import React, { useEffect, useMemo } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import { useDispatch, useSelector } from 'react-redux'
import fetchRun from '../actions/fetchRun.js'
import fetchRunArtifacts from '../actions/fetchRunArtifacts.js'
import Panel from '../components/Panel.js'
import ErrorInlay from '../components/ErrorInlay.jsx'
import SpinnerInlay from '../components/SpinnerInlay.jsx'
import EmptyInlay from '../components/EmptyInlay.jsx'
import RagIndicator from '../components/RagIndicator.js'
import StatTile from '../components/StatTile.jsx'
import ArtifactPanel from '../components/ArtifactPanel.jsx'
import Link from '../components/Link.js'
import TextSpan, { TextDiv } from '../components/TextSpan.js'
import { statusToColor } from '../utils/status-color.js'
import { formatDuration, formatRelative, formatTimestamp } from '../utils/time.js'
import { runDurationMs, runFinishedAt, runStartedAt } from '../utils/runStats.js'
import { LabelArtifact, LabelRunner, LabelWorker } from '../utils/labels.js'

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
    font-size: 1.25rem;
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

const SectionHeader = styled.div`
  display: flex;
  flex-direction: row;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 0.75rem;
`

// The run records who executed it, set when a worker claims the job.
//
// The fallback reads the same identity from artifact labels, which is where this
// came from before the run carried it. It is kept for runs written by an older
// server, and drops away once no such runs remain in retention.
const executorFrom = (run, artifacts) => {
  const executor = run?.status?.executor
  if (executor?.runnerName || executor?.workerName) {
    return { runner: executor.runnerName || null, worker: executor.workerName || null }
  }

  const labelled = (artifacts || []).find((a) => a.metadata?.labels?.[LabelRunner.Name])
  const labels = labelled?.metadata?.labels || {}

  return {
    runner: labels[LabelRunner.Name] || null,
    worker: labels[LabelWorker.Name] || null,
  }
}

const RunDetail = ({ scenarioId, runId }) => {
  const dispatch = useDispatch()
  const key = `${scenarioId}/${runId}`

  const run = useSelector((s) => s.run[key]) || {}
  const artifacts = useSelector((s) => s.runArtifacts[runId]) || {}

  useEffect(() => {
    dispatch(fetchRun(scenarioId, runId))
    dispatch(fetchRunArtifacts(runId))
  }, [scenarioId, runId])

  const artifactList = useMemo(() => artifacts.response?.data || [], [artifacts.response])
  const executor = useMemo(() => executorFrom(run.response, artifactList), [run.response, artifactList])

  if (run.error) {
    return <ErrorInlay message="Error loading run" details={run.error.message || ''} />
  }

  if (!run.response) {
    return <SpinnerInlay />
  }

  const result = run.response
  const started = runStartedAt(result)
  const finished = runFinishedAt(result)

  // Logs first: they are what an operator opens when a run went wrong.
  const ordered = [...artifactList].sort((a, b) => {
    const kindOf = (x) => x.metadata?.labels?.[LabelArtifact.Kind] || ''
    if (kindOf(a) === 'log') return -1
    if (kindOf(b) === 'log') return 1
    return kindOf(a).localeCompare(kindOf(b))
  })

  return (
    <PageContainer>
      <Panel>
        <HeaderRow>
          <RagIndicator color={statusToColor(result.status)} />
          <h2>{result.name}</h2>
          <TextSpan size="small" level={3}>
            <Link href={`/scenarios/${scenarioId}`}>← {scenarioId}</Link>
          </TextSpan>
        </HeaderRow>

        <StatsRow>
          <StatTile
            caption="Outcome"
            value={result.status?.result || result.status?.status || 'unknown'}
            color={statusToColor(result.status)}
            detail={result.status?.result ? `job ${result.status.status}` : null}
          />
          <StatTile
            caption="Started"
            value={started ? formatRelative(started) : '—'}
            detail={started ? formatTimestamp(started) : null}
          />
          <StatTile
            caption="Duration"
            value={formatDuration(runDurationMs(result))}
            detail={finished ? `finished ${formatRelative(finished)}` : 'not finished'}
          />
          <StatTile caption="Type" value={result.spec?.probKind || '—'} />
          <StatTile
            caption="Runner"
            value={executor.runner || '—'}
            detail={executor.worker ? `worker ${executor.worker}` : null}
          />
        </StatsRow>
      </Panel>

      <Panel>
        <SectionHeader>
          <TextSpan size="large" weight={500} level={2}>
            Artifacts
          </TextSpan>
          <TextSpan size="small" level={4}>
            {ordered.length} produced
          </TextSpan>
        </SectionHeader>

        {artifacts.fetching && !artifacts.response && <SpinnerInlay />}
        {artifacts.error && (
          <ErrorInlay message="Error loading artifacts" details={artifacts.error.message || ''} />
        )}
        {!artifacts.fetching && !artifacts.error && ordered.length === 0 && <EmptyInlay />}

        {ordered.map((artifact, i) => (
          <ArtifactPanel key={artifact.metadata.uid} artifact={artifact} defaultOpen={i === 0} />
        ))}
      </Panel>

      <Panel>
        <TextDiv size="small" level={4}>
          Network path, request timing and traces will appear here for prob kinds that can report
          them. Not yet implemented.
        </TextDiv>
      </Panel>
    </PageContainer>
  )
}

RunDetail.propTypes = {
  scenarioId: PropTypes.string.isRequired,
  runId: PropTypes.string.isRequired,
}

export default RunDetail
