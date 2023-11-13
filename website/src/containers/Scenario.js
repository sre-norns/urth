import React, { useState, useCallback } from 'react';
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import OddContainer from '../components/OddContainer.js'
import Capsule from '../components/Capsule.js'
import RagIndicator from '../components/RagIndicator.js'
import TextSpan, {TextDiv} from '../components/TextSpan.js'
import {cyrb53} from '../utils/hash.js'
import Button from '../components/Button.js'
import Link from '../components/Link.js'
import {apiPut} from '../utils/api.js'


const TopContainer = styled.div`
  display: flex;
  flex-direction: row;
`

const BodyContainer = styled.div`
  flex-grow: 1;
`

const ActionsContainer = styled.div`
`

const CapsulesContainer = styled.div`
  display: flex;
  flex-direction: row;
  flex-wrap: wrap;
  gap: .25rem .5rem;
  padding-top: .25rem;
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

const onNonClick = (e) => { e.preventDefault() }

const colors = ['primary', 'secondary', 'error', 'success', 'warning', 'neutral']

const statusToColor = (status) => {
  switch (status) {
    case 'success': return 'success'
    case 'failure': return 'error'
    case 'running': return 'primary'
    case 'pending': return 'warning'
    default: return 'neutral'
  }
}

const Scenario = ({data, odd}) => {
  const {ID, name, labels} = data.metadata
  const {active, description, schedule} = data.spec

  const lastRunStatus = 'unknown'
  const statusColor = statusToColor(lastRunStatus)

  const playDisabled = !active
  const stopDisabled = !active

  const [isSending, setIsSending] = useState(false)

  const requestRun = useCallback(async () => {
    // don't send again while we are sending
    if (isSending) return

    // update state
    setIsSending(true)

    // send the actual request
    try {
      const runRequest = await apiPut(`/api/v1/scenarios/${ID}/results`, {
          token: "fsd"
      })
      
    } catch (error) {
      console.log("Failed to post: ", error);
    }

    setIsSending(false)
  }, [isSending]) // update the callback if the state changes


  return (
    <OddContainer odd={odd}>
      <TopContainer>
        <BodyContainer>
          <TextDiv size="medium" level={2} weight={500}>
            <RagIndicator color={statusColor} style={{margin: '0 .5rem 0 2px'}} />
            <Link href="#" onClick={onNonClick}>{description}</Link>
          </TextDiv>
          <TextDiv size='small' level={4}>
            <TextSpan>Schedule: </TextSpan>
            <TextSpan level={2} weight={500}>{schedule}</TextSpan>
            {/*<TextSpan aria-hidden> · </TextSpan>*/}
            {/*<TextSpan>Last run: </TextSpan>*/}
            {/*<TextSpan level={2} weight={500}>{data.lastRun && data.lastRun.date.toLocaleString() || 'never'}</TextSpan>*/}
            <TextSpan aria-hidden> · </TextSpan>
            <TextSpan>Status: </TextSpan>
            <TextSpan level={2} weight={500}>{lastRunStatus}</TextSpan>
          </TextDiv>
        </BodyContainer>
        <ActionsContainer>
          <PlayButton color="contrast" disabled={playDisabled || isSending} onClick={requestRun}><i className="fi fi-play"></i></PlayButton>
          <StopButton color={stopDisabled ? 'contrast' : 'error'} disabled={stopDisabled}><i className="fi fi-stop"></i></StopButton>
        </ActionsContainer>
      </TopContainer>
      <CapsulesContainer>
        { Object.entries(labels || {}).map(([name, value], i) =>
          <Capsule
            key={name}
            name={name}
            value={value}
            color={colors[cyrb53(name, 11) % colors.length]}
            href="#"
            onClick={onNonClick}
          />
        )}
      </CapsulesContainer>
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
