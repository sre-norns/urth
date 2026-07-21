import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import RagIndicator from './RagIndicator.js'
import TextSpan, { TextDiv } from './TextSpan.js'
import Link from './Link.js'
import { statusToColor } from '../utils/status-color.js'
import { formatDuration, formatRelative, formatTimestamp } from '../utils/time.js'
import { runDurationMs, runStartedAt } from '../utils/runStats.js'

const List = styled.div`
  display: flex;
  flex-direction: column;
`

const Row = styled.div`
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.25rem;
  border-bottom: 1px solid ${(props) => props.theme.color.neutral[props.theme.dark ? 800 : 200]};

  &:last-of-type {
    border-bottom: none;
  }
`

const Outcome = styled.div`
  min-width: 7rem;
`

const When = styled.div`
  flex-grow: 1;
`

const Numeric = styled.div`
  min-width: 5rem;
  text-align: right;
`

const outcomeLabel = (run) => {
  const { status, result } = run.status || {}
  if (!result) {
    return status || 'unknown'
  }

  return result
}

const RunHistoryList = ({ runs, scenarioId, ...rest }) => (
  <List {...rest}>
    {runs.map((run) => {
      const started = runStartedAt(run)

      return (
        <Row key={run.uid || run.name}>
          <RagIndicator color={statusToColor(run.status)} />
          <Outcome>
            <TextSpan size="small" weight={500} level={2} color={statusToColor(run.status)}>
              {outcomeLabel(run)}
            </TextSpan>
          </Outcome>
          <When>
            <TextDiv size="small" level={2}>
              <Link href={`/scenarios/${scenarioId}/runs/${run.name}`}>{formatRelative(started)}</Link>
            </TextDiv>
            <TextDiv size="small" level={4}>
              {formatTimestamp(started)}
            </TextDiv>
          </When>
          <Numeric>
            <TextSpan size="small" level={3}>
              {formatDuration(runDurationMs(run))}
            </TextSpan>
          </Numeric>
          <Numeric>
            <TextSpan size="small" level={4}>
              {run.status?.numberArtifacts || 0} artifacts
            </TextSpan>
          </Numeric>
        </Row>
      )
    })}
  </List>
)

RunHistoryList.propTypes = {
  runs: PropTypes.arrayOf(PropTypes.object).isRequired,
  scenarioId: PropTypes.string.isRequired,
}

export default RunHistoryList
