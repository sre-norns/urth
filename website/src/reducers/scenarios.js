import ActionType from '../actions/ActionType.js'

const initialState = {
  fetching: false,
  scenarios: '',
  error: null,
}

export default (state = initialState, action) => {
  switch (action.type) {
    case ActionType.SCENARIOS_FETCHING:
      return {
        ...state,
        fetching: true,
      }

    case ActionType.SCENARIOS_FETCHED:
      return {
        ...state,
        fetching: false,
        scenarios: action.scenarios,
      }

    case ActionType.SCENARIOS_FETCH_FAILED:
      return {
        ...state,
        fetching: false,
        error: action.error,
      }

    default:
      return state
  }
}