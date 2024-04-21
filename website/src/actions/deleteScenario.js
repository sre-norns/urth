import ActionType from './ActionType.js'
import {apiDelete} from '../utils/api.js'

const deleteScenario = (id, version, successCallback) => async (dispatch) => {
  dispatch({type: ActionType.SCENARIO_DELETING, id})

  try {
    await apiDelete(`/api/v1/scenarios/${id}?version=${version}`)

    dispatch({
      type: ActionType.SCENARIO_DELETED,
      id,
    })

    if (successCallback) {
      successCallback()
    }
  } catch (error) {
    dispatch({
      type: ActionType.SCENARIO_DELETE_FAILED,
      id,
      error,
    })
  }
}

export default deleteScenario
