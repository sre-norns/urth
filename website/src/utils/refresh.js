// Resource detail pages poll while mounted because server-owned status can
// change without any browser action. Keep one cadence so runner and worker
// liveness never appear to refresh at subtly different rates.
export const RESOURCE_REFRESH_INTERVAL_MS = 15000
