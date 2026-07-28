import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'

// Where a run of this scenario would go, if one were triggered now.
//
// Read before offering the run rather than after it: a scenario whose
// requirements match no active runner produces a run that is terminal the
// moment it exists, and a button that reliably creates a failed run is worse
// than no button.
const fetchScenarioPlacement = (id) => async (dispatch) => {
  dispatch({type: ActionType.SCENARIO_PLACEMENT_FETCHING, key: id})

  try {
    const response = await apiGet(`/api/v1/scenarios/${id}/placement`)

    dispatch({type: ActionType.SCENARIO_PLACEMENT_FETCHED, key: id, response})
  } catch (error) {
    dispatch({type: ActionType.SCENARIO_PLACEMENT_FETCH_FAILED, key: id, error})
  }
}

export default fetchScenarioPlacement
