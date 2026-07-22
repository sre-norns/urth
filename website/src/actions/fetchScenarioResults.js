import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'

const fetchScenarioResults = (id, searchQuery) => async (dispatch) => {
  dispatch({type: ActionType.SCENARIO_RESULTS_FETCHING, id})

  try {
    // Note: this test was inverted -- a search query was dropped, and its
    // absence appended "?undefined" to the URL.
    const response = searchQuery
      ? await apiGet(`/api/v1/scenarios/${id}/results?${searchQuery}`)
      : await apiGet(`/api/v1/scenarios/${id}/results`)

    dispatch({
      type: ActionType.SCENARIO_RESULTS_FETCHED,
      id,
      response,
    })
  } catch (error) {
    dispatch({
      type: ActionType.SCENARIO_RESULTS_FETCH_FAILED,
      id,
      error,
    })
  }
}

export default fetchScenarioResults
