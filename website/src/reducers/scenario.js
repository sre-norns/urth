import ActionType from '../actions/ActionType.js'


const initialState = {
  id: '',
  fetching: false,
  response: '',
  error: null,
}

export default (state = initialState, action) => {
  switch (action.type) {
    case ActionType.SCENARIO_FETCHING:
      return {
        ...state,
        id: action.id,
        fetching: true,
        error: null,
        response: state.id === action.id ? state.response : '',
      }

    case ActionType.SCENARIO_FETCHED:
      return state.id !== action.id ? state : {
        ...state,
        fetching: false,
        response: action.response,
      }

    case ActionType.SCENARIO_FETCH_FAILED:
      return state.id !== action.id ? state : {
        ...state,
        fetching: false,
        error: action.error,
      }

    default:
      return state
  }
}