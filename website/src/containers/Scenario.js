import React, { useCallback } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import { parseExpression } from 'cron-parser'
import { Tooltip } from 'react-tooltip'
import { useDispatch, useSelector } from 'react-redux'
import OddContainer from '../components/OddContainer.js'
import Capsule from '../components/Capsule.js'
import RagIndicator from '../components/RagIndicator.js'
import TextSpan, { TextDiv } from '../components/TextSpan.js'
import { cyrb53 } from '../utils/hash.js'
import Button from '../components/Button.js'
import Link from '../components/Link.js'
import runScenario from '../actions/runScenario.js'
import ObjectCapsules from '../components/ObjectCapsules.js'

const TopContainer = styled.div`
  display: flex;
  flex-direction: row;
`

const BodyContainer = styled.div`
  flex-grow: 1;
`

const ActionsContainer = styled.div``

const IconButton = styled(Button)`
  //padding: 1px 5px;
  i {
    padding: 0 4px;
  }
`

IconButton.defaultProps = {
  size: 'small',
  // variant: 'outlined',
}

const PlayButton = styled(IconButton)`
  border-radius: 0.5rem 0 0 0.5rem;
`

const StopButton = styled(IconButton)`
  border-radius: 0 0.5rem 0.5rem 0;
`

const ScenarioCapsules = styled(ObjectCapsules)`
  padding-top: 0.25rem;
`

const statusToColor = (status) => {
  switch (status) {
    case 'pending/':
      return 'warning'
    case 'running/':
      return 'primary'
    case 'completed/success':
      return 'success'
    case 'completed/failure':
      return 'error'
    case 'completed/errored':
      return 'error'
    case 'completed/canceled':
      return 'error'
    case 'completed/timeout':
      return 'error'
    default:
      return 'neutral'
  }
}

function scheduleBreakdown(schedule, status) {
  if (!schedule) {
    return {
      runSchedule: null,
      prevScheduledRun: null,
      nextScheduledRun: null,
    }
  }

  return {
    runSchedule: schedule,
    nextScheduledRun: status.nextScheduledRunTime,
    prevScheduledRun: (status.results && status.results.length > 0)
      ? status.results[0].spec?.start_time || status.results[0].creationTimestamp
      : null
  }

  // try {
  //   const runSchedule = parseExpression(expression)
  //   return {
  //     runSchedule: runSchedule,
  //     prevScheduledRun: runSchedule.prev(),
  //     nextScheduledRun: runSchedule.next(),
  //   }
  // } catch {
  //   return {
  //     runSchedule: null,
  //     prevScheduledRun: null,
  //     nextScheduledRun: null,
  //   }

  // }
}

const Scenario = ({ data, odd, onCapsuleClick }) => {
  const { metadata, spec, status } = data
  const { uid, name, labels } = metadata
  const { active, description, schedule, prob } = spec

  const lastRunStatus = status.results && status.results.length > 0
    ? `${status.results[0].status.status}/${status.results[0].status.result}`
    : 'new';

  const statusColor = statusToColor(lastRunStatus)

  const executable = !!prob?.kind
  const playDisabled = !(active && executable)
  const stopDisabled = !(active && executable)

  const scenarioActions = useSelector((s) => s.scenarioActions)
  const { fetching, response, error } = scenarioActions[name] || {}

  const runSchedule = scheduleBreakdown(schedule, status)
  const dispatch = useDispatch()

  const requestRun = useCallback((event) => {
    event.preventDefault()
    dispatch(runScenario(name))
  }, [])

  return (
    <OddContainer odd={odd}>
      <TopContainer>
        <Tooltip id="schedule-tooltip" effect="solid">
          <TextDiv size="small" level={2} weight={500}>
            <TextSpan>Next run: </TextSpan>
            <TextSpan level={2} weight={500}>
              {runSchedule?.nextScheduledRun?.toString() || 'unknown'}
            </TextSpan>
          </TextDiv>
          <TextDiv size="small" level={2} weight={500}>
            <TextSpan>Previous run: </TextSpan>
            <TextSpan level={2} weight={500}>
              {runSchedule?.prevScheduledRun?.toString() || 'unknown'}
            </TextSpan>
          </TextDiv>
        </Tooltip>

        <BodyContainer>
          <TextDiv size="medium" level={2} weight={500}>
            <RagIndicator color={statusColor} style={{ margin: '0 .5rem 0 2px' }} />
            <Link href={`/scenarios/${name}`}>{name}</Link>
          </TextDiv>
          <TextDiv size="small" level={4}>
            <TextSpan>Schedule: </TextSpan>
            <TextSpan level={2} weight={500} data-tooltip-id="schedule-tooltip">
              {schedule}
            </TextSpan>

            {/*<TextSpan aria-hidden> · </TextSpan>*/}
            {/*<TextSpan>Last run: </TextSpan>*/}
            {/*<TextSpan level={2} weight={500}>{data.lastRun && data.lastRun.date.toLocaleString() || 'never'}</TextSpan>*/}
            <TextSpan aria-hidden> · </TextSpan>
            <TextSpan>Status: </TextSpan>
            <TextSpan level={2} weight={500} color={statusColor}>
              {lastRunStatus}
            </TextSpan>
          </TextDiv>
        </BodyContainer>
        <ActionsContainer>
          <PlayButton color="contrast" disabled={playDisabled || fetching} onClick={requestRun}>
            <i className="fi fi-play"></i>
          </PlayButton>
          <StopButton color={stopDisabled ? 'contrast' : 'error'} disabled={stopDisabled}>
            <i className="fi fi-stop"></i>
          </StopButton>
        </ActionsContainer>
      </TopContainer>
      <ScenarioCapsules value={labels} onCapsuleClick={onCapsuleClick} />
    </OddContainer>
  )
}

const ManifestMeta = PropTypes.shape({
  name: PropTypes.string.isRequired,
  labels: PropTypes.any,
  uid: PropTypes.string,
  version: PropTypes.number,
  creationTimestamp: PropTypes.string,
  updateTimestamp: PropTypes.string,
})

const ScenarioManifest = PropTypes.shape({
  kind: PropTypes.string,
  metadata: ManifestMeta.isRequired,
  spec: PropTypes.shape({
    active: PropTypes.bool,
    description: PropTypes.string,
    schedule: PropTypes.string,
    prob: PropTypes.shape({
      kind: PropTypes.string.isRequired,
      spec: PropTypes.any,
    }),
    requirements: PropTypes.shape({
      matchLabels: PropTypes.any,
      matchSelector: PropTypes.arrayOf(PropTypes.any),
    }),
  }).isRequired,
  status: PropTypes.shape({
    nextScheduledRunTime: PropTypes.string,
    results: PropTypes.arrayOf(PropTypes.any),
  }).isRequired,
})

Scenario.propTypes = {
  data: ScenarioManifest.isRequired,
  odd: PropTypes.bool,
  onCapsuleClick: PropTypes.func
}

export default Scenario
