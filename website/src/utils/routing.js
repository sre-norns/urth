import React from 'react'
import {useLocation, useRoute} from 'wouter'

export const routed = (WrappedComponent, replace = false) => {
  return React.forwardRef((props, ref) => {
    const [, setLocation] = useLocation()
    const handleClick = (e) => {
      // ignores the navigation when clicked using right mouse button or
      // by holding a special modifier key: ctrl, command, win, alt, shift
      if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey || e.button !== 0) {
        return
      }

      const href = props.href
      if (href) {
        e.preventDefault()
        setLocation(href, {replace})
      }
    }

    return <WrappedComponent ref={ref} onClick={handleClick} {...props} />
  })
}

export const routeActivated = (WrappedComponent) => {
  return React.forwardRef((props, ref) => {
    const [match, params] = useRoute(props.activePattern || props.href)
    return <WrappedComponent ref={ref} active={match} {...props} />
  })
}
