import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import TextSpan, { TextDiv } from './TextSpan.js'

const Tile = styled.div`
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  min-width: 8rem;
`

const Value = styled(TextDiv)`
  font-size: 1.5rem;
  line-height: 1.75rem;
`

// A stat is a caption, a number, and optionally the context needed to read the
// number -- "of 42 runs" under a success rate, so it is clear whether the figure
// rests on a large sample or a single run.
const StatTile = ({ caption, value, detail, color = 'neutral', ...rest }) => (
  <Tile {...rest}>
    <TextDiv size="small" level={4}>
      {caption}
    </TextDiv>
    <Value level={2} weight={500} color={color}>
      {value}
    </Value>
    {detail && (
      <TextSpan size="small" level={4}>
        {detail}
      </TextSpan>
    )}
  </Tile>
)

StatTile.propTypes = {
  caption: PropTypes.string.isRequired,
  value: PropTypes.node.isRequired,
  detail: PropTypes.node,
  color: PropTypes.string,
}

export default StatTile
