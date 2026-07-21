import React from 'react'
import { useDispatch, useSelector } from 'react-redux'
import styled from '@emotion/styled'
import { useSearchParams } from 'wouter-search'
import fetchResults from '../actions/fetchResults.js'
import SpinnerInlay from '../components/SpinnerInlay.jsx'
import EmptyInlay from '../components/EmptyInlay.jsx'
import ErrorInlay from '../components/ErrorInlay.jsx'
import RunResult from '../containers/RunResult.jsx'
import { SearchQuery } from '../utils/searchQuery.js'
import { Operator, Rule } from '../utils/k8s-labels.js'

const ResourceContainer = styled.div`
  width: 100%;
  max-width: 1320px;
  margin-left: auto;
  margin-right: auto;
  padding: 1rem;
`

// Runs from every scenario, newest first. This is the view for "something broke
// and I do not know which scenario yet"; a single scenario's history lives on
// its own page.
const Results = () => {
  const [searchParams, setSearchParams] = useSearchParams()

  const dispatch = useDispatch()
  const { fetching, response, error } = useSelector((s) => s.results)

  React.useEffect(() => {
    dispatch(fetchResults(searchParams))
  }, [searchParams])

  const onCapsuleClick = (name, value) => {
    setSearchParams((q) => {
      try {
        const query = new SearchQuery(q)
        query.setRule(new Rule(name, Operator.Equals, [value]))

        return query.urlSearchParams
      } catch (error) {
        console.log('Failed to update search query', error)
        return q
      }
    })
  }

  if (error) {
    return <ErrorInlay message={'Error fetching results'} details={error.message || ''} />
  }

  if (!response || fetching) {
    return <SpinnerInlay />
  }

  if (!Array.isArray(response.data) || !response.data.length) {
    return <EmptyInlay />
  }

  return (
    <ResourceContainer>
      {response.data.map((r, i) => (
        <RunResult key={r.uid || r.name} data={r} odd={i % 2 !== 0} onCapsuleClick={onCapsuleClick} />
      ))}
    </ResourceContainer>
  )
}

export default Results
