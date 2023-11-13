export class ApiError extends Error {
  response
  data

  constructor(message, response, data) {
    super(message)

    this.response = response
    this.data = data
  }
}

const createRequestInit = (method, data) => {
  const token = localStorage.getItem('token')
  const headersInit = token ? {Authorization: 'Bearer ' + token} : {}

  const requestInit = {
    method,
    headers: headersInit,
    mode: 'same-origin',
    cache: 'no-cache',
    credentials: 'omit',
    redirect: 'error',
    referrerPolicy: 'no-referrer',
  }

  return typeof data !== 'undefined' ?
    {
      ...requestInit,
      headers: {
        ...requestInit.headers,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(data),
    } : requestInit
}

const readJson = async (response) => {
  const contentType = response.headers.get('Content-Type')
  const isJson = contentType && contentType.indexOf('application/json') >= 0
  return isJson ? await response.json() : undefined
}

const raiseError = (response, data) => {
  if (response.status >= 200 && response.status < 300) {
    if (data === undefined && response.status !== 204)
      throw new ApiError('Unexpected response format', response, data)
  } else {
    const message =
      data && data.message ? String(data.message) : `Server returned error status ${response.status}`

    throw new ApiError(message, response, data)
  }
}

const performFetch = async (url, init) => {
  const response = await fetch(url, init)
  const data = await readJson(response)
  raiseError(response, data)

  return data
}

export const apiGet = (url) => performFetch(url, createRequestInit('GET'))

export const apiPost = (url, data) => performFetch(url, createRequestInit('POST', data))

export const apiPut = (url, data) => performFetch(url, createRequestInit('PUT', data))

export const apiDelete = (url) => performFetch(url, createRequestInit('DELETE'))
