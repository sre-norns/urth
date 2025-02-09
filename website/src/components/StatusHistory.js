import React, { forwardRef, useMemo } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import { useDispatch, useSelector } from 'react-redux'
import { Tooltip } from 'react-tooltip'
import TextSpan, { TextDiv } from '../components/TextSpan.js'
import SpinnerInlay from '../components/SpinnerInlay.js'
import ErrorInlay from '../components/ErrorInlay.js'
import { statusToColor } from '../utils/status-color.js'
import fetchScenarioResults from '../actions/fetchScenarioResults.js'

const color = (props) => props.theme.color[props.color || 'neutral']

const backgroundColor = (props) => {
    const _color = color(props)
    return props.theme.dark ? _color[400] : _color[500]
}

const StatusHistoryContainer = styled.div`
  display: flex;
  flex-direction: row-reverse;
  flex-wrap: wrap;
  gap: 0.25rem 0.5rem;
`

const StatusBox = styled.div`
  width: 0.5em;
  height: 2em;
//   padding: 15px 6px 20px;
  background-color: ${backgroundColor};
  border-radius: 0.5rem;
  margin-left: -0.3em;
  margin-top: 0.7em;
  transition: 0.4s;
  &:hover {
    margin-top: -0.2em;
    box-shadow: 2;
  }
`

StatusBox.propTypes = {
    color: PropTypes.oneOf(['primary', 'secondary', 'error', 'success', 'warning', 'neutral']),
}

const timeDistance = (date1, date2) => {
    let distance = Math.abs(date1 - date2);
    const hours = Math.floor(distance / 3600000);
    distance -= hours * 3600000;
    const minutes = Math.floor(distance / 60000);
    distance -= minutes * 60000;
    const seconds = Math.floor(distance / 1000);
    return seconds > 0
        ? `${hours}:${('0' + minutes).slice(-2)}:${('0' + seconds).slice(-2)}`
        : `${Math.abs(date1 - date2)}ms`;
}


const Status = ({ result }) => {
    const { metadata, spec, status } = result

    return (
        <div>
            {result.name && (<Tooltip id={`${result.name}-tooltip`} effect="solid">
                {status.status && (<TextDiv size="small" level={2} weight={500}>
                    <TextSpan>Job: </TextSpan>
                    <TextSpan level={2} weight={500}>{status.status || 'unknown'}</TextSpan>
                </TextDiv>)}
                {status.result && (<TextDiv size="small" level={2} weight={500}>
                    <TextSpan>Result: </TextSpan>
                    <TextSpan level={2} weight={500}>{status.result || 'unknown'}</TextSpan>
                </TextDiv>)}
                {spec?.probKind && (<TextDiv size="small" level={2} weight={500}>
                    <TextSpan>Prob: </TextSpan>
                    <TextSpan level={2} weight={500}>{spec.probKind || 'unknown'}</TextSpan>
                </TextDiv>)}

                {spec.end_time
                    ? (
                        <TextDiv size="small" level={2} weight={500}>
                            <TextSpan>Duration: </TextSpan>
                            <TextSpan level={2} weight={500}>
                                {timeDistance(Date.parse(spec.end_time), Date.parse(spec.start_time))}
                            </TextSpan>
                        </TextDiv>
                    )
                    : (
                        <TextDiv size="small" level={2} weight={500}>
                            <TextSpan>Started: </TextSpan>
                            <TextSpan level={2} weight={500}>
                                {spec.start_time || 'unknown'}
                            </TextSpan>
                        </TextDiv>

                    )
                }
            </Tooltip>)}

            <StatusBox data-tooltip-id={result.name ? `${result.name}-tooltip` : ""}
                color={statusToColor(status)}
            />
        </div>
    )
}

const StatusHistory = forwardRef(({ value, limit, ...props }, ref) => {
    const { metadata, } = value
    const { uid, name } = metadata

    const dispatch = useDispatch()
    const scenarioResults = useSelector((s) => s.scenarioResults)
    const { fetching, response, error } = scenarioResults[name] || {}

    React.useEffect(() => {
        dispatch(fetchScenarioResults(name))
    }, [])


    if (error) {
        return <ErrorInlay message={'Error fetching results'} details={error.message || ''} />
    }

    if (!response || fetching) {
        return <SpinnerInlay />
    }

    const statuses = (response?.data || []).slice(0, limit)
    if (statuses) {
        statuses.sort((b, a) => Date.parse(a.creationTimestamp) - Date.parse(b.creationTimestamp))
    }
    if (statuses.length < limit) {
        for (let i = statuses.length; i < limit; i++) {
            statuses.push({
                status: {
                    status: "fake"
                }
            })
        }
    }

    return (
        <StatusHistoryContainer {...props} ref={ref}>
            {statuses.map((status, i) => (
                <Status key={status.name || i} result={status}
                // name={name}
                // value={value}
                // color={colors[cyrb53(name, 11) % colors.length]}
                // href="#"
                // onClick={clickHandlers[name]}
                />
            ))}
        </StatusHistoryContainer>
    )
})

StatusHistory.propTypes = {
    value: PropTypes.object,
    limit: PropTypes.number.isRequired,
    //   onCapsuleClick: PropTypes.func,
}

export default StatusHistory
