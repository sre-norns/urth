import React from 'react'
import {useDispatch, useSelector} from 'react-redux'
import styled from '@emotion/styled'
import {useSearchParams} from 'wouter-search'
import fetchDeadLetters, {retryDeadLetter, resolveDeadLetter} from '../actions/fetchDeadLetters.js'
import SpinnerInlay from '../components/SpinnerInlay.jsx'
import EmptyInlay from '../components/EmptyInlay.jsx'
import ErrorInlay from '../components/ErrorInlay.jsx'
import Button from '../components/Button.js'
import Link from '../components/Link.js'
import {
  UNRESOLVED_SELECTOR,
  deadLetterRows,
  hasResolvedFilter,
  withUnresolvedFilter,
} from '../utils/deadLetters.js'

const ResourceContainer = styled.div`
  width: 100%;
  max-width: 1320px;
  margin-left: auto;
  margin-right: auto;
  padding: 1rem;
`

const Header = styled.div`
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1rem;
`

const Row = styled.div`
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1fr) auto;
  gap: 0.75rem;
  align-items: center;
  padding: 0.75rem 0.5rem;
  border-bottom: 1px solid ${(props) => props.theme.shade.divider || 'rgba(128,128,128,0.25)'};
  background-color: ${(props) => (props.odd ? 'rgba(128,128,128,0.05)' : 'transparent')};
`

const HeaderRow = styled(Row)`
  font-weight: 600;
  border-bottom-width: 2px;
`

const Cell = styled.div`
  overflow: hidden;
  text-overflow: ellipsis;
`

const Detail = styled.div`
  grid-column: 1 / -1;
  font-size: 0.85em;
  opacity: 0.75;
  overflow-wrap: anywhere;
`

const Actions = styled.div`
  display: flex;
  gap: 0.5rem;
`

const Notice = styled.p`
  margin: 0.5rem 0;
`

// Dispatches that stopped making progress: a message nobody could parse, one
// delivered to the wrong runner, a claim permanently refused, or a delivery
// count the broker gave up on. Each leaves a run that will never start, and
// before this page the only account of why was a line in a worker's log.
const DeadLetters = () => {
  const [searchParams, setSearchParams] = useSearchParams()
  const dispatch = useDispatch()
  const {fetching, response, error, acting, actionError, lastAction} = useSelector((s) => s.deadLetters)

  // Unresolved by default. Somebody arriving here is asking what is still
  // broken, and a list that accumulates every failure ever handled answers a
  // different question.
  const showingResolved = hasResolvedFilter(searchParams)

  React.useEffect(() => {
    dispatch(fetchDeadLetters(withUnresolvedFilter(searchParams)))
  }, [searchParams])

  const toggleResolved = () => {
    setSearchParams((q) => {
      const params = new URLSearchParams(q)
      if (showingResolved) {
        params.delete('all')
      } else {
        params.set('all', 'true')
      }

      return params
    })
  }

  const act = async (action, name) => {
    try {
      await dispatch(action(name))
    } finally {
      // Refetched rather than patched in place: a retry changes the failure and
      // creates a run, and guessing at the new state client-side is how the two
      // surfaces drift apart.
      dispatch(fetchDeadLetters(withUnresolvedFilter(searchParams)))
    }
  }

  if (error) {
    return <ErrorInlay message={'Error fetching dispatch failures'} details={error.message || ''} />
  }

  if (!response || fetching) {
    return <SpinnerInlay />
  }

  const rows = deadLetterRows(response)

  return (
    <ResourceContainer>
      <Header>
        <h2>Dead letters</h2>
        <Button size="small" variant="outlined" onClick={toggleResolved}>
          {showingResolved ? 'Show unresolved only' : 'Show resolved too'}
        </Button>
      </Header>

      {actionError && <ErrorInlay message={'Action failed'} details={actionError.message || ''} />}

      {lastAction?.response?.retry?.metadata?.name && (
        <Notice>
          Retried as run{' '}
          <Link href={`/results/${lastAction.response.retry.metadata.name}`}>
            {lastAction.response.retry.metadata.name}
          </Link>
          .
        </Notice>
      )}

      {!rows.length ? (
        showingResolved ? (
          <EmptyInlay />
        ) : (
          // Distinguished from "nothing matched" on purpose: an empty table
          // reads identically for both, and only one of them is good news.
          <Notice>No unresolved dispatch failures.</Notice>
        )
      ) : (
        <div>
          <HeaderRow as="div">
            <Cell>Dispatch</Cell>
            <Cell>Reason</Cell>
            <Cell>Scenario</Cell>
            <Cell>Runner</Cell>
            <Cell>Status</Cell>
          </HeaderRow>

          {rows.map((row, i) => (
            <Row key={row.name} odd={i % 2 !== 0}>
              <Cell title={row.name}>{row.name}</Cell>
              <Cell>{row.reason}</Cell>
              <Cell>
                {row.scenarioName ? (
                  <Link href={`/scenarios/${row.scenarioName}`}>{row.scenarioName}</Link>
                ) : (
                  '-'
                )}
              </Cell>
              <Cell>{row.runnerName || '-'}</Cell>
              <Actions>
                {row.retryResultName ? (
                  <Link href={`/results/${row.retryResultName}`}>retried</Link>
                ) : row.resolved ? (
                  <span>resolved</span>
                ) : (
                  <>
                    <Button
                      size="small"
                      color="primary"
                      disabled={Boolean(acting?.[row.name])}
                      onClick={() => act(retryDeadLetter, row.name)}
                    >
                      Retry
                    </Button>
                    <Button
                      size="small"
                      variant="outlined"
                      disabled={Boolean(acting?.[row.name])}
                      onClick={() => act(resolveDeadLetter, row.name)}
                    >
                      Resolve
                    </Button>
                  </>
                )}
              </Actions>
              {row.detail && <Detail>{row.detail}</Detail>}
            </Row>
          ))}
        </div>
      )}
    </ResourceContainer>
  )
}

export {UNRESOLVED_SELECTOR}
export default DeadLetters
