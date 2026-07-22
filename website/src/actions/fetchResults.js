import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'

// Runs across every scenario, as opposed to fetchScenarioResults which is scoped
// to one. This is what the Results page reads: finding a failure usually starts
// without knowing which scenario produced it.
const fetchResults = (searchQuery) => async (dispatch) => {
  dispatch({type: ActionType.RESULTS_FETCHING})

  try {
    const query = searchQuery ? `?${searchQuery}` : ''
    const response = await apiGet(`/api/v1/results${query}`)

    dispatch({type: ActionType.RESULTS_FETCHED, response})
  } catch (error) {
    dispatch({type: ActionType.RESULTS_FETCH_FAILED, error})
  }
}

export default fetchResults
