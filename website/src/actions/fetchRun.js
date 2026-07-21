import ActionType from './ActionType.js'
import { apiGet } from '../utils/api.js'

const fetchRun = (scenarioId, runId) => async (dispatch) => {
  const key = `${scenarioId}/${runId}`
  dispatch({ type: ActionType.RUN_FETCHING, key })

  try {
    const response = await apiGet(`/api/v1/scenarios/${scenarioId}/results/${runId}`)

    dispatch({ type: ActionType.RUN_FETCHED, key, response })
  } catch (error) {
    dispatch({ type: ActionType.RUN_FETCH_FAILED, key, error })
  }
}

export default fetchRun
