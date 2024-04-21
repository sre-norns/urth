import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'

const fetchScenario = (id) => async (dispatch) => {
  dispatch({type: ActionType.SCENARIO_FETCHING, id})

  try {
    const response = await apiGet(`/api/v1/scenarios/${id}`)

    dispatch({
      type: ActionType.SCENARIO_FETCHED,
      id,
      response,
    })
  } catch (error) {
    dispatch({
      type: ActionType.SCENARIO_FETCH_FAILED,
      id,
      error,
    })
  }
}

export default fetchScenario
