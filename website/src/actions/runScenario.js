import ActionType from './ActionType.js'
import { apiPost } from '../utils/api.js'

const runScenario = (name) => async (dispatch) => {
  dispatch({ type: ActionType.RUN_SCENARIO_FETCHING, name })

  try {
    const response = await apiPost(`/api/v1/scenarios/${name}/results`, {
      metadata: {
        name: 'manual-',
        labels: {
          trigger: 'manual',
          triggerAgent: 'web-ui',
        },
      },
    })

    dispatch({
      type: ActionType.RUN_SCENARIO_FETCHED,
      name,
      response,
    })
  } catch (error) {
    dispatch({
      type: ActionType.RUN_SCENARIO_FETCH_FAILED,
      name,
      error,
    })
  }
}

export default runScenario
