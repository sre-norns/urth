import React from 'react'
import { useDispatch, useSelector } from 'react-redux'
import styled from '@emotion/styled'
import { useSearchParams } from 'wouter-search';
import fetchScenarios from '../actions/fetchScenarios.js'
import SpinnerInlay from '../components/SpinnerInlay.js'
import Scenario from '../containers/Scenario.js'
import EmptyInlay from '../components/EmptyInlay.js'
import ErrorInlay from '../components/ErrorInlay.js'
import { SearchQuery } from '../utils/searchQuery.js';
import { Operator, Rule } from '../utils/k8s-labels.js'

const ScenariosContainer = styled.div`
  width: 100%;
  max-width: 1320px;
  margin-left: auto;
  margin-right: auto;
  padding: 1rem;
`

const Scenarios = () => {
  const [searchParams, setSearchParams] = useSearchParams();

  const dispatch = useDispatch()
  const { fetching, response, error } = useSelector((s) => s.scenarios)

  React.useEffect(() => {
    dispatch(fetchScenarios(searchParams))
  }, [searchParams])

  if (error) {
    return <ErrorInlay message={'Error fetching scenarios'} details={error.message || ''} />
  }

  if (!response || fetching) {
    return <SpinnerInlay />
  }

  if (!Array.isArray(response.data) || !response.data.length) {
    return <EmptyInlay />
  }

  const onCapsuleClick = (name, value) => {
    setSearchParams((q) => {
      try {
        const query = new SearchQuery(q)
        query.setRule(new Rule(name, Operator.Equals, [value]));
        console.log("Updating search query", query.labels)

        return query.urlSearchParams
      } catch (error) {
        console.log("Failed to update search query", error)

      }
    })
  }

  return (
    <ScenariosContainer>
      {response.data.map((s, i) => (
        <Scenario key={s.metadata.uid} data={s} odd={i % 2 !== 0} onCapsuleClick={onCapsuleClick} />
      ))}
    </ScenariosContainer>
  )
}

export default Scenarios
