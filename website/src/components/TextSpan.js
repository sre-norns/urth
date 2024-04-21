import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const color = (props) => props.theme.color[props.color || 'neutral']

const levelToShadeIndex = (level) => (!level ? 50 : 100 * level)

const textColor = (props) => {
  const _color = color(props)
  const _level = props.level
  if (typeof _level !== 'number') {
    return 'unset'
  }

  const baseShadeIndex = levelToShadeIndex(_level)
  const correctedShadeIndex = props.theme.dark ? baseShadeIndex : 1000 - baseShadeIndex
  return _color[correctedShadeIndex]
}

const fontSize = (props) => {
  switch (props.size) {
    case 'small':
      return '0.875rem'
    case 'medium':
      return '1rem'
    case 'large':
      return '1.25rem'
    default:
      return 'unset'
  }
}

const fontWeight = (props) => props.weight || 'unset'

const lineHeight = (props) => {
  switch (props.size) {
    case 'small':
      return '1.25rem'
    case 'medium':
      return '1.5rem'
    case 'large':
      return '1.75rem'
    default:
      return 'unset'
  }
}

const TextSpan = styled.span`
  color: ${textColor};
  font-size: ${fontSize};
  font-weight: ${fontWeight};
  line-height: ${lineHeight};
`

TextSpan.propTypes = {
  color: PropTypes.string,
  level: PropTypes.oneOf([0, 1, 2, 3, 4, 5]),
  size: PropTypes.oneOf(['small', 'medium', 'large']),
  weight: PropTypes.oneOf(['normal', 'bold', 100, 200, 300, 400, 500, 600, 700, 800, 900]),
}

export const TextDiv = TextSpan.withComponent('div')

export default TextSpan
