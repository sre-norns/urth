import ActionType from '../actions/ActionType.js'

const initialState = {}

export default (state = initialState, action) => {
    switch (action.type) {
        case ActionType.SCENARIO_RESULTS_FETCHING:
            return {
                ...state,
                [action.id]: {
                    ...state[action.id],
                    fetching: true,
                    error: null,
                },
            }

        case ActionType.SCENARIO_RESULTS_FETCHED:
            return {
                ...state,
                [action.id]: {
                    ...state[action.id],
                    fetching: false,
                    response: action.response,
                },
            }

        case ActionType.SCENARIO_RESULTS_FETCH_FAILED:
            return {
                ...state,
                [action.id]: {
                    ...state[action.id],
                    fetching: false,
                    error: action.error,
                },
            }

        default:
            return state
    }
}
