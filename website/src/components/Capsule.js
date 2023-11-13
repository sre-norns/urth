import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const color = (props) => props.theme.color[props.color || 'neutral']

const tintedBackgroundColor = (props, state) => {
  const _color = color(props)
  if (props.theme.dark) {
    switch (state) {
      case 'hover': return _color[300]
      case 'active': return _color[500]
      case 'disabled': return _color[800]
      default: return _color[400]
    }
  } else {
    switch (state) {
      case 'hover': return _color[600]
      case 'active': return _color[700]
      case 'disabled': return _color[200]
      default: return _color[500]
    }
  }
}

const invertedBackgroundColor = (props, state) => {
  const _color = color(props)
  const _background = props.theme.shade.background
  if (props.theme.dark) {
    switch (state) {
      case 'hover': return _color[950]
      case 'active': return _color[900]
      case 'disabled': return _background
      default: return _background
      }
  } else {
    switch (state) {
      case 'hover': return _color[50]
      case 'active': return _color[100]
      case 'disabled': return _background
      default: return _background
      }
  }
}

const tintedTextColor = (props, state) => {
  const _color = color(props)
  return props.theme.dark ?
    (state === 'disabled' ? _color[100] : _color[950]) :
    (state === 'disabled' ? _color[700] : _color[50])
}

const invertedTextColor = (props, state) => {
  const _color = color(props)
  return props.theme.dark ? _color[300] : _color[700]
}

const tintedBorder = (props, state) => {
  const shade = tintedBackgroundColor(props, state)
  return `1px solid ${shade}`
}

const NameSpan = styled.span`
  padding: 0 .25rem 0 .5rem;
`

const ValueSpan = styled.span`
  padding: 0 .5rem 0 .25rem;
`

const CapsuleHref = styled.a`
  font-size: 0.875rem;
  line-height: 1rem;
  text-decoration: none;
  background-color: ${tintedBackgroundColor};
  border: ${tintedBorder};
  border-radius: 0.75rem;
  margin: 1px 0;
  overflow: hidden;
  
  ${NameSpan} {
    color: ${tintedTextColor};
  }
  
  ${ValueSpan} {
    background-color: ${invertedBackgroundColor};
    color: ${invertedTextColor};
  }
  
  &:hover {
    border: ${props => tintedBorder(props, 'hover')};
    
    ${NameSpan} {
      background-color: ${props => tintedBackgroundColor(props, 'hover')};
    }
    
    ${ValueSpan} {
      background-color: ${props => invertedBackgroundColor(props, 'hover')};
    }
  }
  
  &:active {
    border: ${props => tintedBorder(props, 'active')};
    
    ${NameSpan} {
      background-color: ${props => tintedBackgroundColor(props, 'active')};
    }
    
    ${ValueSpan} {
      background-color: ${props => invertedBackgroundColor(props, 'active')};
    }
  }
`

const Capsule = React.forwardRef(({name, value, ...props}, ref) => {
  return (
    <CapsuleHref {...props} ref={ref}>
      <NameSpan>{name}</NameSpan>
      <ValueSpan>{value}</ValueSpan>
    </CapsuleHref>
  )
})

Capsule.propTypes = {
  name: PropTypes.node,
  value: PropTypes.node,
}

export default Capsule