import ActionType from './ActionType.js'
import { apiGet } from '../utils/api.js'

const fetchScenarioResults = (id, searchQuery) => async (dispatch) => {
    dispatch({ type: ActionType.SCENARIO_RESULTS_FETCHING, id })

    try {
        const response = searchQuery
            ? await apiGet(`/api/v1/scenarios/${id}/results`)
            : await apiGet(`/api/v1/scenarios/${id}/results?${searchQuery}`)

        dispatch({
            type: ActionType.SCENARIO_RESULTS_FETCHED,
            id,
            response,
        })
    } catch (error) {
        dispatch({
            type: ActionType.SCENARIO_RESULTS_FETCH_FAILED,
            id,
            error,
        })
    }
}

export default fetchScenarioResults
