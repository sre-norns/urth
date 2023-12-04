import ActionType from './ActionType.js'
import {apiPut} from '../utils/api.js'


const updateScenario = (id, version, data) => async dispatch => {
  dispatch({type: ActionType.SCENARIO_UPDATING, id})

  try {
    const response = await apiPut(`/api/v1/scenarios/${id}?version=${version}`, data)

    dispatch({
      type: ActionType.SCENARIO_UPDATED,
      id,
      response,
    })
  } catch (error) {
    dispatch({
      type: ActionType.SCENARIO_UPDATE_FAILED,
      id,
      error,
    })
  }
}

export default updateScenario