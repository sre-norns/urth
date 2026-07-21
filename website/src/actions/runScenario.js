import ActionType from './ActionType.js'
import { apiPost } from '../utils/api.js'

// onQueued fires once the API has accepted the run. The run itself is executed
// asynchronously by a worker, so this signals "queued", not "finished" -- it is
// the point at which it is worth re-reading the run history.
const runScenario = (name, onQueued) => async (dispatch) => {
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

    if (onQueued) {
      onQueued(response)
    }
  } catch (error) {
    dispatch({
      type: ActionType.RUN_SCENARIO_FETCH_FAILED,
      name,
      error,
    })
  }
}

export default runScenario
