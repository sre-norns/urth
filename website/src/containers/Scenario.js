import React, { useCallback } from 'react';
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {parseExpression} from 'cron-parser';
import { Tooltip } from 'react-tooltip'
import {useDispatch, useSelector} from 'react-redux'
import OddContainer from '../components/OddContainer.js'
import Capsule from '../components/Capsule.js'
import RagIndicator from '../components/RagIndicator.js'
import TextSpan, {TextDiv} from '../components/TextSpan.js'
import {cyrb53} from '../utils/hash.js'
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

const ActionsContainer = styled.div`
`

const IconButton = styled(Button)`
  //padding: 1px 5px;
  i {
    padding: 0 4px
  }
`

IconButton.defaultProps = {
  size: 'small',
  // variant: 'outlined',
}

const PlayButton = styled(IconButton)`
  border-radius: .5rem 0 0 .5rem;
`

const StopButton = styled(IconButton)`
  border-radius: 0 .5rem .5rem 0;
`

const ScenarioCapsules = styled(ObjectCapsules)`
  padding-top: .25rem;
`

const statusToColor = (status) => {
  switch (status) {
    case 'success': return 'success'
    case 'failure': return 'error'
    case 'running': return 'primary'
    case 'pending': return 'warning'
    default: return 'neutral'
  }
}

function scheduleBreakdown(expression) {
  if (!expression) {
    return {
      runSchedule: null,
      prevScheduledRun: null,
      nextScheduledRun: null
    }
  }

  const runSchedule = parseExpression(expression)
  return {
    runSchedule: runSchedule,
    prevScheduledRun: runSchedule.prev(),
    nextScheduledRun: runSchedule.next()
  }
}

const Scenario = ({data, odd}) => {
  const {ID, name, labels} = data.metadata
  const {active, description, schedule} = data.spec

  const lastRunStatus = 'unknown'
  const statusColor = statusToColor(lastRunStatus)

  const playDisabled = !active
  const stopDisabled = !active

  const scenarioActions = useSelector(s => s.scenarioActions)
  const {fetching, response, error} = scenarioActions[ID] || {}

  const runSchedule = scheduleBreakdown(schedule)
  const dispatch = useDispatch()

  const requestRun = useCallback((event) => {
    event.preventDefault()
    dispatch(runScenario(ID))
  }, [])

  return (
    <OddContainer odd={odd}>
      <TopContainer>
      <Tooltip id="schedule-tooltip" effect="solid" >
        <TextDiv size="small" level={2} weight={500}>
          <TextSpan>Next run: </TextSpan>
          <TextSpan level={2} weight={500}>{runSchedule?.nextScheduledRun?.toString() || "unknown"}</TextSpan>
        </TextDiv>
        <TextDiv size="small" level={2} weight={500}>
          <TextSpan>Previous run: </TextSpan>
          <TextSpan level={2} weight={500}>{runSchedule?.prevScheduledRun?.toString() || "unknown"}</TextSpan>
        </TextDiv>
      </Tooltip>

        <BodyContainer>
          <TextDiv size="medium" level={2} weight={500}>
            <RagIndicator color={statusColor} style={{margin: '0 .5rem 0 2px'}} />
            <Link href={`/scenarios/${ID}`}>{name}</Link>
          </TextDiv>
          <TextDiv size='small' level={4}>
            <TextSpan>Schedule: </TextSpan>
            <TextSpan level={2} weight={500} data-tooltip-id="schedule-tooltip">{schedule}</TextSpan>

            
            {/*<TextSpan aria-hidden> · </TextSpan>*/}
            {/*<TextSpan>Last run: </TextSpan>*/}
            {/*<TextSpan level={2} weight={500}>{data.lastRun && data.lastRun.date.toLocaleString() || 'never'}</TextSpan>*/}
            <TextSpan aria-hidden> · </TextSpan>
            <TextSpan>Status: </TextSpan>
            <TextSpan level={2} weight={500}>{lastRunStatus}</TextSpan>
          </TextDiv>
        </BodyContainer>
        <ActionsContainer>
          <PlayButton color="contrast" disabled={playDisabled || fetching} onClick={requestRun}><i className="fi fi-play"></i></PlayButton>
          <StopButton color={stopDisabled ? 'contrast' : 'error'} disabled={stopDisabled}><i className="fi fi-stop"></i></StopButton>
        </ActionsContainer>
      </TopContainer>
      <ScenarioCapsules value={labels} />
    </OddContainer>
  )
}

Scenario.propTypes = {
  data: PropTypes.shape({
    kind: PropTypes.oneOf(['scenarios']).isRequired,
    metadata: PropTypes.shape({
      ID: PropTypes.oneOfType([PropTypes.number, PropTypes.string]).isRequired,
      name: PropTypes.string.isRequired,
    }).isRequired,
    spec: PropTypes.shape({
      description: PropTypes.string,
      active: PropTypes.bool,
      schedule: PropTypes.string,
    }).isRequired,
  }).isRequired,
  odd: PropTypes.bool,
}

export default Scenario
