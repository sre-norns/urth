import ActionType from './ActionType.js'
import { apiGet } from '../utils/api.js'

const fetchScenarios = (searchQuery) => async (dispatch) => {
  dispatch({ type: ActionType.SCENARIOS_FETCHING })

  try {
    const query = searchQuery?.toString()
    const response = (query)
      ? await apiGet(`/api/v1/scenarios?${query}`)
      : await apiGet(`/api/v1/scenarios?${query}`)

    dispatch({
      type: ActionType.SCENARIOS_FETCHED,
      response,
    })
  } catch (error) {
    dispatch({
      type: ActionType.SCENARIOS_FETCH_FAILED,
      error,
    })
  }
}

export default fetchScenarios
