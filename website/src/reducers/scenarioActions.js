import ActionType from '../actions/ActionType.js'

const initialState = {}

export default (state = initialState, action) => {
  switch (action.type) {
    // reset cache in case of page refresh
    case ActionType.SCENARIOS_FETCHED:
      return {}

    case ActionType.RUN_SCENARIO_FETCHING:
      return {
        ...state,
        [action.id]: {
          ...state[action.id],
          fetching: true,
          error: null,
        },
      }

    case ActionType.RUN_SCENARIO_FETCHED:
      return {
        ...state,
        [action.id]: {
          ...state[action.id],
          fetching: false,
          response: action.response,
        },
      }

    case ActionType.RUN_SCENARIO_FETCH_FAILED:
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
