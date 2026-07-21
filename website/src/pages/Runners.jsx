import React from 'react'
import { useDispatch, useSelector } from 'react-redux'
import styled from '@emotion/styled'
import { useSearchParams } from 'wouter-search'
import fetchRunners from '../actions/fetchRunners.js'
import SpinnerInlay from '../components/SpinnerInlay.jsx'
import Runner from '../containers/Runner.jsx'
import EmptyInlay from '../components/EmptyInlay.jsx'
import ErrorInlay from '../components/ErrorInlay.jsx'
import { SearchQuery } from '../utils/searchQuery.js'
import { Operator, Rule } from '../utils/k8s-labels.js'

const ResourceContainer = styled.div`
  width: 100%;
  max-width: 1320px;
  margin-left: auto;
  margin-right: auto;
  padding: 1rem;
`

const Runners = () => {
  const [searchParams, setSearchParams] = useSearchParams()

  const dispatch = useDispatch()
  // Note: this read from the scenarios slice, which fetchRunners also wrote to,
  // so the two lists replaced one another on navigation.
  const { fetching, response, error } = useSelector((s) => s.runners)

  React.useEffect(() => {
    dispatch(fetchRunners(searchParams))
  }, [searchParams])

  // Clicking a label narrows the list to it -- the same gesture as the scenario
  // list, so the two pages behave alike.
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
    return <ErrorInlay message={'Error fetching runners'} details={error.message || ''} />
  }

  if (!response || fetching) {
    return <SpinnerInlay />
  }

  if (!Array.isArray(response.data) || !response.data.length) {
    return <EmptyInlay />
  }

  return (
    <ResourceContainer>
      {response.data.map((s, i) => (
        <Runner key={s.metadata.uid} data={s} odd={i % 2 !== 0} onCapsuleClick={onCapsuleClick} />
      ))}
    </ResourceContainer>
  )
}

export default Runners
