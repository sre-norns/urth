import React, { useCallback, useEffect, useMemo, useState } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import { useDispatch, useSelector } from 'react-redux'
import fetchScenario from '../actions/fetchScenario.js'
import fetchScenarioResults from '../actions/fetchScenarioResults.js'
import runScenario from '../actions/runScenario.js'
import Panel from '../components/Panel.js'
import Button from '../components/Button.js'
import ErrorInlay from '../components/ErrorInlay.jsx'
import SpinnerInlay from '../components/SpinnerInlay.jsx'
import EmptyInlay from '../components/EmptyInlay.jsx'
import ObjectCapsules from '../components/ObjectCapsules.jsx'
import RagIndicator from '../components/RagIndicator.js'
import StatTile from '../components/StatTile.jsx'
import RunHistoryList from '../components/RunHistoryList.jsx'
import TextSpan, { TextDiv } from '../components/TextSpan.js'
import { routed } from '../utils/routing.jsx'
import { statusToColor } from '../utils/status-color.js'
import { formatDuration, formatPercent, formatRelative, formatTimestamp } from '../utils/time.js'
import { PERIODS, Period, filterByPeriod, summariseRuns } from '../utils/runStats.js'

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
`

const PeriodRow = styled.div`
  display: flex;
  flex-direction: row;
  align-items: baseline;
  gap: 0.5rem;
`

const PeriodButton = styled.button`
  background: none;
  border: none;
  cursor: pointer;
  padding: 0.125rem 0.25rem;
  font: inherit;
  font-size: 0.875rem;
  font-weight: ${(props) => (props.selected ? 600 : 400)};
  text-decoration: ${(props) => (props.selected ? 'underline' : 'none')};
  color: ${(props) => props.theme.color.neutral[props.theme.dark ? 200 : 800]};
`

const SectionHeader = styled.div`
  display: flex;
  flex-direction: row;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 0.5rem;
`

const EditLink = routed(
  styled(Button)`
    i {
      padding: 0 0.5rem 0 0;
    }
  `.withComponent('a'),
  true
)

const ScenarioDetail = ({ scenarioId }) => {
  const dispatch = useDispatch()
  const [period, setPeriod] = useState(Period.Week)

  const { id, fetching, error, response: scenario } = useSelector((s) => s.scenario)
  const results = useSelector((s) => s.scenarioResults[scenarioId]) || {}
  const scenarioActions = useSelector((s) => s.scenarioActions)
  const pendingRun = scenarioActions[scenarioId]?.fetching

  useEffect(() => {
    dispatch(fetchScenario(scenarioId))
    dispatch(fetchScenarioResults(scenarioId))
  }, [scenarioId])

  // A manual run is queued asynchronously, so the new run only appears once the
  // worker has picked it up. Re-reading the history after the request settles is
  // what makes the button feel like it did something.
  const handleRun = useCallback(() => {
    dispatch(runScenario(scenarioId, () => dispatch(fetchScenarioResults(scenarioId))))
  }, [scenarioId])

  const runs = useMemo(() => results.response?.data || [], [results.response])
  const runsInPeriod = useMemo(() => filterByPeriod(runs, period), [runs, period])
  const summary = useMemo(() => summariseRuns(runsInPeriod), [runsInPeriod])

  if (error) {
    return <ErrorInlay message="Error loading scenario" details={error.message || ''} />
  }

  if (fetching || !scenario || id !== scenarioId) {
    return <SpinnerInlay />
  }

  const { metadata, spec, status } = scenario
  const lastRunStatus = status?.results?.[0]?.status
  const executable = Boolean(spec?.prob?.kind)
  const runnable = Boolean(spec?.active) && executable

  return (
    <PageContainer>
      <Panel>
        <HeaderRow>
          <RagIndicator color={lastRunStatus ? statusToColor(lastRunStatus) : 'neutral'} />
          <h2>{metadata.name}</h2>
          <Button onClick={handleRun} disabled={!runnable || pendingRun} color="contrast">
            <i className="fi fi-play"></i> {pendingRun ? 'Queueing…' : 'Run now'}
          </Button>
          <EditLink href={`/scenarios/${scenarioId}/edit`} color="neutral">
            <i className="fi fi-page-edit"></i> Edit
          </EditLink>
        </HeaderRow>

        {spec?.description && (
          <TextDiv size="small" level={3} style={{ marginTop: '0.5rem' }}>
            {spec.description}
          </TextDiv>
        )}

        <ObjectCapsules value={metadata.labels} style={{ paddingTop: '0.5rem' }} />

        <StatsRow style={{ marginTop: '1rem' }}>
          <StatTile caption="Type" value={spec?.prob?.kind || 'none'} />
          <StatTile caption="Schedule" value={spec?.schedule || 'unscheduled'} />
          <StatTile
            caption="Next run"
            value={status?.nextScheduledRunTime ? formatRelative(status.nextScheduledRunTime) : '—'}
            detail={status?.nextScheduledRunTime ? formatTimestamp(status.nextScheduledRunTime) : null}
          />
          <StatTile caption="State" value={spec?.active ? 'active' : 'disabled'} color={spec?.active ? 'success' : 'neutral'} />
        </StatsRow>

        {!executable && (
          <TextDiv size="small" level={3} color="warning" style={{ marginTop: '0.75rem' }}>
            This scenario has no prob defined, so it cannot be run.
          </TextDiv>
        )}
      </Panel>

      <Panel>
        <SectionHeader>
          <TextSpan size="large" weight={500} level={2}>
            Statistics
          </TextSpan>
          <PeriodRow>
            {PERIODS.map((p) => (
              <PeriodButton key={p.id} selected={p.id === period} onClick={() => setPeriod(p.id)}>
                {p.label}
              </PeriodButton>
            ))}
          </PeriodRow>
        </SectionHeader>

        <StatsRow>
          <StatTile caption="Runs" value={summary.total} detail={`in the last ${PERIODS.find((p) => p.id === period).label.toLowerCase()}`} />
          <StatTile
            caption="Success rate"
            value={formatPercent(summary.successRate)}
            detail={summary.settled ? `${summary.succeeded} of ${summary.settled} settled` : 'no completed runs'}
            color={summary.successRate === null ? 'neutral' : summary.successRate === 1 ? 'success' : 'warning'}
          />
          <StatTile caption="Failures" value={summary.failed} color={summary.failed ? 'error' : 'neutral'} />
          <StatTile caption="Average duration" value={formatDuration(summary.averageDurationMs)} />
        </StatsRow>
      </Panel>

      <Panel>
        <SectionHeader>
          <TextSpan size="large" weight={500} level={2}>
            Run history
          </TextSpan>
          <TextSpan size="small" level={4}>
            {runsInPeriod.length} of {runs.length} runs
          </TextSpan>
        </SectionHeader>

        {results.fetching && !results.response && <SpinnerInlay />}
        {results.error && <ErrorInlay message="Error loading run history" details={results.error.message || ''} />}
        {!results.fetching && !results.error && runsInPeriod.length === 0 && <EmptyInlay />}
        {runsInPeriod.length > 0 && <RunHistoryList runs={runsInPeriod} scenarioId={scenarioId} />}
      </Panel>
    </PageContainer>
  )
}

ScenarioDetail.propTypes = {
  scenarioId: PropTypes.string.isRequired,
}

export default ScenarioDetail
