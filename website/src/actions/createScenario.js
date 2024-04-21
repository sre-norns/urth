import ActionType from './ActionType.js'
import {apiPost} from '../utils/api.js'

const createScenario = (data, successCallback) => async (dispatch) => {
  dispatch({type: ActionType.SCENARIO_CREATING})

  try {
    const response = await apiPost(`/api/v1/scenarios`, data)
    const id = response.metadata.ID

    dispatch({
      type: ActionType.SCENARIO_CREATED,
      id,
      response,
    })

    if (successCallback) {
      successCallback(id, response)
    }
  } catch (error) {
    dispatch({
      type: ActionType.SCENARIO_CREATE_FAILED,
      error,
    })
  }
}

export default createScenario
