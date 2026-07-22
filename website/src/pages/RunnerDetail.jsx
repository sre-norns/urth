import React, {useCallback, useEffect, useMemo, useState} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {useDispatch, useSelector} from 'react-redux'
import fetchRunner from '../actions/fetchRunner.js'
import fetchWorkers from '../actions/fetchWorkers.js'
import {apiPut} from '../utils/api.js'
import Panel from '../components/Panel.js'
import Button from '../components/Button.js'
import ErrorInlay from '../components/ErrorInlay.jsx'
import SpinnerInlay from '../components/SpinnerInlay.jsx'
import EmptyInlay from '../components/EmptyInlay.jsx'
import ObjectCapsules from '../components/ObjectCapsules.jsx'
import RagIndicator from '../components/RagIndicator.js'
import StatTile from '../components/StatTile.jsx'
import WorkerList from '../components/WorkerList.jsx'
import TextSpan, {TextDiv} from '../components/TextSpan.js'
import {formatRelative} from '../utils/time.js'

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

const SectionHeader = styled.div`
  display: flex;
  flex-direction: row;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 0.5rem;
`

const Requirement = styled.div`
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8125rem;
  padding: 0.125rem 0;
`

// Renders a runner's requirements the way they are written in a manifest, so
// what is shown matches what an operator would edit.
const requirementLines = (requirements) => {
  if (!requirements) {
    return []
  }

  const lines = Object.entries(requirements.matchLabels || {}).map(([k, v]) => `${k} = ${v}`)

  for (const rule of requirements.matchSelector || []) {
    const values = (rule.values || []).join(',')
    lines.push(values ? `${rule.key} ${rule.operator} (${values})` : `${rule.key} ${rule.operator}`)
  }

  return lines
}

const RunnerDetail = ({runnerId}) => {
  const dispatch = useDispatch()
  const [busy, setBusy] = useState(false)

  const runner = useSelector((s) => s.runner[runnerId]) || {}
  const workers = useSelector((s) => s.workers[runnerId]) || {}

  useEffect(() => {
    dispatch(fetchRunner(runnerId))
    dispatch(fetchWorkers(runnerId))
  }, [runnerId])

  const workerList = useMemo(() => workers.response?.data || [], [workers.response])
  const pausedCount = useMemo(() => workerList.filter((w) => w.status?.paused).length, [workerList])

  const manifest = runner.response

  // Disabling a runner stops new workers registering and stops the ones already
  // connected from claiming further jobs. Runs already in flight are left alone.
  const toggleActive = useCallback(async () => {
    if (!manifest) {
      return
    }

    const next = !manifest.spec?.active
    if (
      !next &&
      !confirm(
        `Disable runner "${runnerId}"?\n\nIts ${workerList.length} worker(s) will stop taking new jobs ` +
          `and no new workers will be able to register. Runs already in progress are unaffected.`
      )
    ) {
      return
    }

    setBusy(true)
    try {
      await apiPut(`/api/v1/runners/${runnerId}`, {
        ...manifest,
        spec: {...manifest.spec, active: next},
      })
      dispatch(fetchRunner(runnerId))
      dispatch(fetchWorkers(runnerId))
    } finally {
      setBusy(false)
    }
  }, [manifest, runnerId, workerList.length])

  if (runner.error) {
    return <ErrorInlay message="Error loading runner" details={runner.error.message || ''} />
  }

  if (!manifest) {
    return <SpinnerInlay />
  }

  const {metadata, spec} = manifest
  const active = Boolean(spec?.active)
  const requirements = requirementLines(spec?.requirements)

  return (
    <PageContainer>
      <Panel>
        <HeaderRow>
          <RagIndicator color={active ? 'success' : 'neutral'} />
          <h2>{metadata.name}</h2>
          <Button onClick={toggleActive} disabled={busy} color={active ? 'error' : 'contrast'}>
            {active ? 'Disable runner' : 'Enable runner'}
          </Button>
        </HeaderRow>

        {spec?.description && (
          <TextDiv size="small" level={3} style={{marginTop: '0.5rem'}}>
            {spec.description}
          </TextDiv>
        )}

        <ObjectCapsules value={metadata.labels} style={{paddingTop: '0.5rem'}} />

        <StatsRow>
          <StatTile
            caption="State"
            value={active ? 'active' : 'disabled'}
            color={active ? 'success' : 'neutral'}
            detail={active ? null : 'workers cannot take jobs'}
          />
          <StatTile
            caption="Workers"
            value={spec?.maxInstance ? `${workerList.length}/${spec.maxInstance}` : workerList.length}
            detail={spec?.maxInstance ? 'registered / limit' : 'registered'}
          />
          <StatTile
            caption="Paused"
            value={pausedCount}
            color={pausedCount ? 'warning' : 'neutral'}
            detail={pausedCount ? 'not taking jobs' : null}
          />
          <StatTile caption="Created" value={formatRelative(metadata.creationTimestamp)} />
        </StatsRow>

        {requirements.length > 0 && (
          <div style={{marginTop: '1rem'}}>
            <TextDiv size="small" level={4}>
              Requirements a worker must satisfy to register
            </TextDiv>
            {requirements.map((line) => (
              <Requirement key={line}>
                <TextSpan size="small" level={2}>
                  {line}
                </TextSpan>
              </Requirement>
            ))}
          </div>
        )}
      </Panel>

      <Panel>
        <SectionHeader>
          <TextSpan size="large" weight={500} level={2}>
            Workers
          </TextSpan>
          <TextSpan size="small" level={4}>
            {workerList.length} claiming this identity
          </TextSpan>
        </SectionHeader>

        <TextDiv size="small" level={4} style={{marginBottom: '0.75rem'}}>
          Every process that authenticated with this runner&apos;s token. Pause one to take it out of service without
          disturbing the rest of the pool; drop one to revoke a registration that should not be here.
        </TextDiv>

        {workers.fetching && !workers.response && <SpinnerInlay />}
        {workers.error && <ErrorInlay message="Error loading workers" details={workers.error.message || ''} />}
        {!workers.fetching && !workers.error && workerList.length === 0 && <EmptyInlay />}
        {workerList.length > 0 && <WorkerList workers={workerList} runnerName={runnerId} />}
      </Panel>
    </PageContainer>
  )
}

RunnerDetail.propTypes = {
  runnerId: PropTypes.string.isRequired,
}

export default RunnerDetail
