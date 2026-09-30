import {useEffect} from 'react'
import {ErrorState, LoadingState} from '@sre-norns/components'
import {useAccountId, useIdentity, useIdentityQuery, useSessionEpoch, wire} from '@sre-norns/components/identity'
import {Outlet, useLocation, useParams} from 'react-router-dom'

/** Validate the project before mounting forms or issuing any product request. */
export function ProjectScope() {
  const {accountId = '', projectId = ''} = useParams()
  const location = useLocation()
  const account = useAccountId()
  const epoch = useSessionEpoch()
  const {paths, queryClient} = useIdentity()
  const project = useIdentityQuery(paths.project(projectId), wire.project, {scope: projectId, enabled: account === accountId})
  useEffect(() => () => {
    void queryClient.cancelQueries({queryKey: [account, projectId]})
    queryClient.removeQueries({queryKey: [account, projectId]})
  }, [account, projectId, queryClient])
  if (project.isError) return <ErrorState error={project.error} retry={() => void project.refetch()} />
  if (!project.data) return <LoadingState />
  if (project.data.data.accountId !== accountId) return <ErrorState error={new Error('This project does not belong to the account in this address.')} />
  return <Outlet key={`${account}:${projectId}:${epoch}:${location.pathname}`} />
}
