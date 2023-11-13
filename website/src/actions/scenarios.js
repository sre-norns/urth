import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'

const delay = (ms) => new Promise(resolve => setTimeout(resolve, ms))

export const fetchScenarios = () => async dispatch => {
  dispatch({type: ActionType.SCENARIOS_FETCHING})

  try {
    const scenarios = await apiGet('/api/v1/scenarios')

    //await delay(500)

    dispatch({
      type: ActionType.SCENARIOS_FETCHED,
      scenarios,
    })
  } catch (error) {
    dispatch({
      type: ActionType.SCENARIOS_FETCH_FAILED,
      error,
    })
  }
}