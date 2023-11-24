import React from 'react'
import {useParams} from 'react-router-dom'
import {useDispatch, useSelector} from 'react-redux'
import fetchScenario from '../actions/fetchScenario.js'
import ErrorInlay from '../components/ErrorInlay.js'
import SpinnerInlay from '../components/SpinnerInlay.js'


const ScenarioEditor = () => {
  const {scenarioId} = useParams()

  const dispatch = useDispatch()

  React.useEffect(() => {
    dispatch(fetchScenario(scenarioId))
  }, [scenarioId])

  const {id, fetching, response, error} = useSelector(s => s.scenario)
  if (id !== scenarioId) {
    return null
  }

  if (error) {
    return <ErrorInlay message={"Error fetching scenarios"} details={error.message || ""}/>
  }

  if (fetching) {
    return <SpinnerInlay/>
  }

  return (
    <div>
      <p>You are here:</p>
      <p>Scenario {scenarioId} editor</p>
      <pre>{JSON.stringify(response, null, 2)}</pre>
    </div>
  )
}

export default ScenarioEditor
