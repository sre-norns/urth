import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import OddContainer from '../components/OddContainer.js'
import RagIndicator from '../components/RagIndicator.js'
import TextSpan, { TextDiv } from '../components/TextSpan.js'
import Link from '../components/Link.js'
import ObjectCapsules from '../components/ObjectCapsules.jsx'
import { statusToColor } from '../utils/status-color.js'
import { formatDuration, formatRelative, formatTimestamp } from '../utils/time.js'
import { runDurationMs, runStartedAt } from '../utils/runStats.js'
import { LabelScenario, LabelWorker } from '../utils/labels.js'

const TopContainer = styled.div`
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 0.75rem;
`

const BodyContainer = styled.div`
  flex-grow: 1;
  min-width: 0;
`

const Numeric = styled.div`
  min-width: 5rem;
  text-align: right;
`

const ResultCapsules = styled(ObjectCapsules)`
  padding-top: 0.25rem;
`

// Almost every label on a run is a system label restating something the row
// already shows -- the scenario, the worker, the outcome, half a dozen UIDs.
// Rendering them all would bury the labels that actually carry information,
// which are the ones inherited from the scenario. These are hidden from the
// capsules; the search bar still filters on them.
const ROW_LABELS = new Set([
  'urth/result.name',
  'urth/result.uid',
  'urth/result.version',
  'urth/result.state',
  'urth/result.result',
  'urth/scenario.name',
  'urth/scenario.uid',
  'urth/scenario.version',
  'urth/scenario.kind',
  'urth/runner.name',
  'urth/runner.uid',
  'urth/runner.version',
  'urth/worker.name',
  'urth/worker.uid',
  'run.messageId',
])

const displayLabels = (labels) =>
  Object.fromEntries(Object.entries(labels || {}).filter(([k]) => !ROW_LABELS.has(k)))

const RunResult = ({ data, odd, onCapsuleClick }) => {
  const { name, labels } = data
  const status = data.status || {}
  const scenarioName = labels?.[LabelScenario.Name]
  const worker = labels?.[LabelWorker.Name] || status.executor?.workerName
  const started = runStartedAt(data)

  return (
    <OddContainer odd={odd}>
      <TopContainer>
        <RagIndicator color={statusToColor(status)} />
        <BodyContainer>
          <TextDiv size="medium" level={2} weight={500}>
            <Link href={`/results/${name}`}>{scenarioName || name}</Link>
            <TextSpan size="small" level={4}>
              {' '}
              {scenarioName ? name : ''}
            </TextSpan>
          </TextDiv>
          <TextDiv size="small" level={4}>
            <TextSpan>Outcome: </TextSpan>
            <TextSpan level={2} weight={500} color={statusToColor(status)}>
              {status.result || status.status || 'unknown'}
            </TextSpan>
            <TextSpan aria-hidden> · </TextSpan>
            <TextSpan>{formatRelative(started)}</TextSpan>
            <TextSpan aria-hidden> · </TextSpan>
            <TextSpan>{formatTimestamp(started)}</TextSpan>
            {worker && (
              <>
                <TextSpan aria-hidden> · </TextSpan>
                <TextSpan>on </TextSpan>
                <TextSpan level={2}>{worker}</TextSpan>
              </>
            )}
          </TextDiv>
        </BodyContainer>
        <Numeric>
          <TextSpan size="small" level={3}>
            {formatDuration(runDurationMs(data))}
          </TextSpan>
        </Numeric>
        <Numeric>
          <TextSpan size="small" level={4}>
            {status.numberArtifacts || 0} artifacts
          </TextSpan>
        </Numeric>
      </TopContainer>
      <ResultCapsules value={displayLabels(labels)} onCapsuleClick={onCapsuleClick} />
    </OddContainer>
  )
}

RunResult.propTypes = {
  // Note: a run comes back flat -- name and labels at the top level -- rather
  // than nested under `metadata` like the other resources.
  data: PropTypes.shape({
    name: PropTypes.string.isRequired,
    uid: PropTypes.string,
    labels: PropTypes.object,
    spec: PropTypes.object,
    status: PropTypes.object,
  }).isRequired,
  odd: PropTypes.bool,
  onCapsuleClick: PropTypes.func,
}

export default RunResult
