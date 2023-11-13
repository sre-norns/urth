import React from 'react'
import {useDispatch, useSelector} from 'react-redux'
import {fetchScenarios} from '../actions/scenarios.js'
import styled from '@emotion/styled'
import SpinnerInlay from '../components/SpinnerInlay.js'
import Scenario from '../containers/Scenario.js'


const ScenariosContainer = styled.div`
  padding: 1rem;
`

const Scenarios = () => {
  const {fetching, scenarios, error} = useSelector(s => s.scenarios)

  const dispatch = useDispatch()

  React.useEffect(() => {
    dispatch(fetchScenarios())
  }, [])

  return (
    (fetching || !scenarios || !Array.isArray(scenarios.data)) ? (
      <SpinnerInlay/>
    ) : (
      <ScenariosContainer>
        {/*Loaded scenarios: {scenarios.count}*/}
        {scenarios.data.map((s, i) =>
          <Scenario key={s.metadata.ID} data={s} odd={i % 2 === 1}/>
        )}
      </ScenariosContainer>
    )
  )
}

export default Scenarios
