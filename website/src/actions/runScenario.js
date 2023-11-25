import ActionType from './ActionType.js'
import {apiPost} from '../utils/api.js'


const runScenario = (id) => async dispatch => {
  dispatch({type: ActionType.RUN_SCENARIO_FETCHING, id})

  try {
    const response = await apiPost(`/api/v1/scenarios/${id}/results`, {
      name: "manual-",
      labels: {
        trigger: "manual",
        triggerAgent: "web-ui"
      },
    })

    dispatch({
      type: ActionType.RUN_SCENARIO_FETCHED,
      id,
      response,
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
