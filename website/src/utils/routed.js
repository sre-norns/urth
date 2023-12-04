import React from 'react'
import {useNavigate} from 'react-router-dom'


const findHref = (element) => {
  if (!element) return null
  if (element.tagName === 'A') return element.href
  return findHref(element.parentElement)
}

const routed = (WrappedComponent, replace = false) => {
  return React.forwardRef((props, ref) => {
    const navigate = useNavigate()
    const handleClick = (e) => {
      const href = findHref(e.target)
      if (href) {
        const url = new URL(href)
        if (url.origin === window.location.origin) {
          e.preventDefault()
          navigate(url.pathname, {replace})
        }
      }
    }

    return <WrappedComponent ref={ref} onClick={handleClick} {...props} />
  })
}

export default routed
