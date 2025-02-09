import React, { useCallback } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
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
import Runners from '../pages/Runners.js'

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

const ScenarioCapsules = styled(ObjectCapsules)`
  padding-top: 0.25rem;
`

const statusToColor = (status) => {
    return status
        ? 'success'
        : 'neutral';
}

const Runner = ({ data, odd }) => {
    const { metadata, spec, status } = data
    const { uid, name, labels } = metadata
    const { description, active, maxInstance, requirements } = spec
    const { numberInstances, activeInstances } = status

    const statusColor = statusToColor(active)

    const instanceCounter = maxInstance
        ? `${numberInstances || 0}/${maxInstance}`
        : `${numberInstances || 0}`;

    return (
        <OddContainer odd={odd}>
            <TopContainer>
                <Tooltip id="schedule-tooltip" effect="solid">
                    {/* <TextSpan>{`${description}`}</TextSpan> */}

                    {/* <TextDiv size="small" level={2} weight={500}>
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
                    </TextDiv> */}
                </Tooltip>

                <BodyContainer>
                    <TextDiv size="medium" level={2} weight={500}>
                        <RagIndicator color={statusColor} style={{ margin: '0 .5rem 0 2px' }} />
                        <Link href={`/runners/${name}`}>{name}</Link>
                    </TextDiv>
                    <TextDiv size="small" level={4}>

                        {/* <TextSpan>Schedule: </TextSpan> */}
                        <TextSpan level={2} weight={500} data-tooltip-id="schedule-tooltip">
                            {description}
                        </TextSpan>

                        {/*<TextSpan aria-hidden> · </TextSpan>*/}
                        {/*<TextSpan>Last run: </TextSpan>*/}
                        {/*<TextSpan level={2} weight={500}>{data.lastRun && data.lastRun.date.toLocaleString() || 'never'}</TextSpan>*/}

                        {/* <TextSpan aria-hidden> · </TextSpan>
                        <TextSpan>Status: </TextSpan>
                        <TextSpan level={2} weight={500}>
                            {lastRunStatus}
                        </TextSpan> */}
                    </TextDiv>
                </BodyContainer>

                <ActionsContainer>
                    <TextSpan level={2} weight={500} data-tooltip-id="schedule-tooltip">
                        {instanceCounter}
                    </TextSpan>

                    {/* <PlayButton color="contrast" disabled={playDisabled || fetching} onClick={requestRun}>
                        <i className="fi fi-play"></i>
                    </PlayButton>
                    <StopButton color={stopDisabled ? 'contrast' : 'error'} disabled={stopDisabled}>
                        <i className="fi fi-stop"></i>
                    </StopButton> */}
                </ActionsContainer>
            </TopContainer>
            <ScenarioCapsules value={labels} />
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

const RunnerManifest = PropTypes.shape({
    kind: PropTypes.string,
    metadata: ManifestMeta.isRequired,
    spec: PropTypes.shape({
        description: PropTypes.string,
        active: PropTypes.bool,
        maxInstance: PropTypes.number,
        requirements: PropTypes.shape({
            matchLabels: PropTypes.any,
            matchSelector: PropTypes.arrayOf(PropTypes.any),
        }),
    }).isRequired,
    status: PropTypes.shape({
        numberInstances: PropTypes.number,
        activeInstances: PropTypes.arrayOf(PropTypes.any),
    }).isRequired,
})

Runner.propTypes = {
    data: RunnerManifest.isRequired,
    odd: PropTypes.bool,
}

export default Runner
