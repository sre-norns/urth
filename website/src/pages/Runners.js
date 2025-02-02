import React from 'react'
import { useDispatch, useSelector } from 'react-redux'
import styled from '@emotion/styled'
import fetchRunners from '../actions/fetchRunners.js'
import SpinnerInlay from '../components/SpinnerInlay.js'
import Runner from '../containers/Runner.js'
import EmptyInlay from '../components/EmptyInlay.js'
import ErrorInlay from '../components/ErrorInlay.js'

const ResourceContainer = styled.div`
  width: 100%;
  max-width: 1320px;
  margin-left: auto;
  margin-right: auto;
  padding: 1rem;
`

const Runners = () => {
    const dispatch = useDispatch()
    const { fetching, response, error } = useSelector((s) => s.scenarios)

    React.useEffect(() => {
        dispatch(fetchRunners())
    }, [])

    if (error) {
        return <ErrorInlay message={'Error fetching scenarios'} details={error.message || ''} />
    }

    if (!response || fetching) {
        return <SpinnerInlay />
    }

    if (!Array.isArray(response.data) || !response.data.length) {
        return <EmptyInlay />
    }

    return (
        <ResourceContainer>
            {response.data.map((s, i) => (
                <Runner key={s.metadata.uid} data={s} odd={i % 2 !== 0} />
            ))}
        </ResourceContainer>
    )
}

export default Runners
