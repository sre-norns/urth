import {useCallback} from 'react'
import {useSearchParams} from 'react-router-dom'
import {PAGE_SIZE} from './utils'

export function useListSearch() {
  const [params, setParams] = useSearchParams()
  const state = {
    name: params.get('name') || '',
    labels: params.get('labels') || '',
    page: Math.max(0, Number(params.get('page')) || 0),
    pageSize: Math.max(1, Number(params.get('pageSize')) || PAGE_SIZE),
  }
  const set = useCallback(
    (key: 'name' | 'labels' | 'page' | 'pageSize', value: string | number) => {
      setParams(
        (current) => {
          const next = new URLSearchParams(current)
          const isDefault =
            ((key === 'name' || key === 'labels') && value === '') ||
            (key === 'page' && value === 0) ||
            (key === 'pageSize' && value === PAGE_SIZE)
          if (isDefault) next.delete(key)
          else next.set(key, String(value))
          if (key !== 'page' && key !== 'pageSize') next.delete('page')
          return next
        },
        {replace: true},
      )
    },
    [setParams],
  )
  return {state, set}
}
