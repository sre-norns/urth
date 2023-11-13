import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const color = (props) => props.theme.color[props.color || 'primary']

const variant = (props) => props.variant || 'contained'

const isOutlined = (props) => variant(props) === 'outlined'

const size = (props) => props.size || 'medium'

const containedBackgroundColor = (props, state) => {
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

const outlinedBackgroundColor = (props, state) => {
  const _color = color(props)
  if (props.theme.dark) {
    switch (state) {
      case 'hover': return _color[950]
      case 'active': return _color[900]
      case 'disabled': return 'transparent'
      default: return 'transparent'
    }
  } else {
    switch (state) {
      case 'hover': return _color[50]
      case 'active': return _color[100]
      case 'disabled': return 'transparent'
      default: return 'transparent'
    }
  }
}

const backgroundColor = (props, state) => {
  switch (variant(props)) {
    case 'contained':
      return containedBackgroundColor(props, state)

    case 'outlined':
      return outlinedBackgroundColor(props, state)
  }
}

const textColor = (props, state) => {
  const _dark = props.theme.dark
  const _color = color(props)
  switch (variant(props)) {
    case 'contained':
      return _dark ?
        (state === 'disabled' ? _color[100] : _color[950]) :
        (state === 'disabled' ? _color[700] : _color[50])

    case 'outlined':
      return _dark ? _color[300] : _color[700]
  }
}

const border = (props) => {
  if (isOutlined(props)) {
    const shade = color(props)[props.theme.dark ? 300 : 700]
    return `1px solid ${shade}`
  } else {
    return 'none'
  }
}

const padding = (props) => {
  const _isOutlined = isOutlined(props);
  switch (size(props)) {
    case 'small': return _isOutlined ? '3px 7px' : '4px 8px'
    case 'medium': return _isOutlined ? '7px 15px' : '8px 16px'
    case 'large': return _isOutlined ? '11px 23px' : '12px 24px'
  }
}

const fontSize = (props) => {
  switch (size(props)) {
    case 'small': return '0.875rem'
    case 'medium': return '1rem'
    case 'large': return '1.25rem'
  }
}

const lineHeight = (props) => {
  switch (size(props)) {
    case 'small': return '1.25rem'
    case 'medium': return '1.5rem'
    case 'large': return '1.75rem'
  }
}

const Button = styled.button`
  display: inline-flex;
  align-items: baseline;
  justify-content: center;
  background-color: ${backgroundColor};
  color: ${textColor};
  font-size: ${fontSize};
  font-weight: 500;
  line-height: ${lineHeight};
  //letter-spacing: 0.02em;
  border: ${border};
  border-radius: .5rem;
  padding: ${padding};
  //text-transform: uppercase;
  cursor: pointer;

  &:hover {
    background-color: ${props => backgroundColor(props, 'hover')};
    color: ${props => textColor(props, 'hover')};
  }

  &:active {
    background-color: ${props => backgroundColor(props, 'active')};
    color: ${props => textColor(props, 'active')};
  }
  
  &:disabled {
    background-color: ${props => backgroundColor(props, 'disabled')};
    color: ${props => textColor(props, 'disabled')};
    opacity: 0.5;
    pointer-events: none;
    cursor: default;
  }
`

Button.propTypes = {
  children: PropTypes.node,
  color: PropTypes.oneOf(['primary', 'secondary', 'success', 'warning', 'error', 'neutral', 'contrast']),
  size: PropTypes.oneOf(['small', 'medium', 'large']),
  variant: PropTypes.oneOf(['contained', 'outlined']),
};

export default Button;