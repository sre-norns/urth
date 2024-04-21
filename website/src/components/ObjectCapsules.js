import React, {forwardRef, useMemo} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import Capsule from './Capsule.js'
import {cyrb53} from '../utils/hash.js'

const colors = ['primary', 'secondary', 'error', 'success', 'warning', 'neutral']

const ObjectCapsulesContainer = styled.div`
  display: flex;
  flex-direction: row;
  flex-wrap: wrap;
  gap: 0.25rem 0.5rem;
`

const ObjectCapsules = forwardRef(({value, onCapsuleClick, ...props}, ref) => {
  const clickHandlers = useMemo(() => {
    const handlers = {}
    Object.entries(value || {}).forEach(([name, value]) => {
      handlers[name] = (e) => {
        e.preventDefault()
        if (onCapsuleClick) {
          onCapsuleClick(name, value)
        }
      }
    })
    return handlers
  }, [value, onCapsuleClick])

  return (
    <ObjectCapsulesContainer {...props} ref={ref}>
      {Object.entries(value || {}).map(([name, value], i) => (
        <Capsule
          key={name}
          name={name}
          value={value}
          color={colors[cyrb53(name, 11) % colors.length]}
          href="#"
          onClick={clickHandlers[name]}
        />
      ))}
    </ObjectCapsulesContainer>
  )
})

ObjectCapsules.propTypes = {
  value: PropTypes.object,
  onCapsuleClick: PropTypes.func,
}

export default ObjectCapsules
