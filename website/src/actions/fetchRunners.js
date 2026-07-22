import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'

// Note: this previously dispatched SCENARIOS_* and wrote into the scenarios
// slice, so opening the runners page replaced the scenario list and vice versa.
const fetchRunners = (searchQuery) => async (dispatch) => {
  dispatch({type: ActionType.RUNNERS_FETCHING})

  try {
    const query = searchQuery ? `?${searchQuery}` : ''
    const response = await apiGet(`/api/v1/runners${query}`)

    dispatch({type: ActionType.RUNNERS_FETCHED, response})
  } catch (error) {
    dispatch({type: ActionType.RUNNERS_FETCH_FAILED, error})
  }
}

export default fetchRunners
