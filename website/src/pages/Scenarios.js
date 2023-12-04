import React from 'react'
import {useDispatch, useSelector} from 'react-redux'
import styled from '@emotion/styled'
import fetchScenarios from '../actions/fetchScenarios.js'
import SpinnerInlay from '../components/SpinnerInlay.js'
import Scenario from '../containers/Scenario.js'
import EmptyInlay from '../components/EmptyInlay.js'
import ErrorInlay from '../components/ErrorInlay.js'


const ScenariosContainer = styled.div`
  width: 100%;
  max-width: 1320px;
  margin-left: auto;
  margin-right: auto;
  padding: 1rem;
`

const Scenarios = () => {
  const dispatch = useDispatch()
  const {fetching, response, error} = useSelector(s => s.scenarios)

  React.useEffect(() => {
    dispatch(fetchScenarios())
  }, [])

  if (error) {
    return <ErrorInlay message={"Error fetching scenarios"} details={error.message || ""}/>
  }

  if (!response || fetching) {
    return <SpinnerInlay/>
  }

  if (!Array.isArray(response.data) || !response.data.length) {
    return <EmptyInlay/>
  }

  return (
    <ScenariosContainer>
      {response.data.map((s, i) =>
        <Scenario key={s.metadata.ID} data={s} odd={i % 2 === 1}/>
      )}
    </ScenariosContainer>
  )
}

export default Scenarios
