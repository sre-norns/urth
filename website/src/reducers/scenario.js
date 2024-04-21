import ActionType from '../actions/ActionType.js'

const initialState = {
  id: '',
  fetching: false,
  creating: false,
  updating: false,
  deleting: false,
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
      return state.id !== action.id
        ? state
        : {
            ...state,
            fetching: false,
            response: action.response,
          }

    case ActionType.SCENARIO_FETCH_FAILED:
      return state.id !== action.id
        ? state
        : {
            ...state,
            fetching: false,
            error: action.error,
          }

    case ActionType.SCENARIO_CREATING:
      return {
        ...state,
        creating: true,
        error: null,
      }

    case ActionType.SCENARIO_CREATED:
      return {
        ...state,
        creating: false,
        id: action.id,
        response: action.response,
      }

    case ActionType.SCENARIO_CREATE_FAILED:
      return {
        ...state,
        creating: false,
        error: action.error,
      }

    case ActionType.SCENARIO_UPDATING:
      return {
        ...state,
        id: action.id,
        updating: true,
        error: null,
      }

    case ActionType.SCENARIO_UPDATED:
      return state.id !== action.id
        ? state
        : {
            ...state,
            updating: false,
            response: action.response,
          }

    case ActionType.SCENARIO_UPDATE_FAILED:
      return state.id !== action.id
        ? state
        : {
            ...state,
            updating: false,
            error: action.error,
          }

    case ActionType.SCENARIO_DELETING:
      return {
        ...state,
        id: action.id,
        deleting: true,
        error: null,
      }

    case ActionType.SCENARIO_DELETED:
      return state.id !== action.id
        ? state
        : {
            ...state,
            deleting: false,
            response: '',
          }

    case ActionType.SCENARIO_DELETE_FAILED:
      return state.id !== action.id
        ? state
        : {
            ...state,
            deleting: false,
            error: action.error,
          }

    default:
      return state
  }
}
