import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'


const fetchScenarios = () => async dispatch => {
  dispatch({type: ActionType.SCENARIOS_FETCHING})

  try {
    const response = await apiGet('/api/v1/scenarios')

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
