import ActionType from './ActionType.js'
import {apiPut} from '../utils/api.js'


const runScenario = (id) => async dispatch => {
  dispatch({type: ActionType.RUN_SCENARIO_FETCHING, id})

  try {
    const result = await apiPut(`/api/v1/scenarios/${id}/results`, {
      token: "fsd"
    })

    dispatch({
      type: ActionType.RUN_SCENARIO_FETCHED,
      id,
      result,
    })
  } catch (error) {
    dispatch({
      type: ActionType.RUN_SCENARIO_FETCH_FAILED,
      id,
      error,
    })
  }
}

export default runScenario
