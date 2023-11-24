import React from 'react'
import {useDispatch, useSelector} from 'react-redux'
import styled from '@emotion/styled'
import fetchScenarios from '../actions/fetchScenarios.js'
import SpinnerInlay from '../components/SpinnerInlay.js'
import Scenario from '../containers/Scenario.js'
import EmptyInlay from '../components/EmptyInlay.js'
import ErrorInlay from '../components/ErrorInlay.js'


const ScenariosContainer = styled.div`
  padding: 1rem;
`

const Scenarios = () => {
  const dispatch = useDispatch()
  const {fetching, scenarios, error} = useSelector(s => s.scenarios)

  React.useEffect(() => {
    dispatch(fetchScenarios())
  }, [])

  if (error) {
    return <ErrorInlay message={"Error fetching scenarios"} details={error.message || ""}/>
  }

  if (fetching) {
    return <SpinnerInlay/>
  }

  if (!scenarios || !Array.isArray(scenarios.data) || !scenarios.data.length) {
    return <EmptyInlay/>
  }

  return (
    <ScenariosContainer>
      {/*Loaded scenarios: {scenarios.count}*/}
      {scenarios.data.map((s, i) =>
        <Scenario key={s.metadata.ID} data={s} odd={i % 2 === 1}/>
      )}
    </ScenariosContainer>
  )
}

export default Scenarios
