import ActionType from './ActionType.js'
import {apiGetText} from '../utils/api.js'

// Artifact content is served raw at its own endpoint rather than inlined in the
// artifact resource, because a HAR recording or a screenshot can be large. It is
// fetched only when something actually wants to display it.
const fetchArtifactContent = (artifactId) => async (dispatch) => {
  dispatch({type: ActionType.ARTIFACT_CONTENT_FETCHING, key: artifactId})

  try {
    const response = await apiGetText(`/api/v1/artifacts/${artifactId}/content`)

    dispatch({type: ActionType.ARTIFACT_CONTENT_FETCHED, key: artifactId, response})
  } catch (error) {
    dispatch({type: ActionType.ARTIFACT_CONTENT_FETCH_FAILED, key: artifactId, error})
  }
}

export default fetchArtifactContent
