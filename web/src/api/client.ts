import type { ListingFilters, ListingsResponse, Snapshot, Stats, Listing } from '../types'

export interface SourceRefreshRequest {
  country_code: string
  city?: string
  operation?: string
  property_type?: string
  bbox: { min_longitude: number; max_longitude: number; min_latitude: number; max_latitude: number }
}

export interface SourceRefreshResponse {
  status: 'SKIPPED' | 'QUEUED' | 'RUNNING' | 'COOLDOWN' | 'UNSUPPORTED' | 'DONE' | 'FAILED'
  refresh_id?: string
  retry_after_seconds?: number
  error?: string
}

const apiRoot = '/api/v1'

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${apiRoot}${path}`, {
    ...options,
    headers: { Accept: 'application/json', ...(options?.headers ?? {}) },
  })
  if (!response.ok) {
    let message = `Error ${response.status}`
    try {
      const payload = (await response.json()) as { error?: string }
      message = payload.error ?? message
    } catch {
      // Keep the status message when the server response is not JSON.
    }
    throw new Error(message)
  }
  return (await response.json()) as T
}

export function fetchListings(filters: ListingFilters, signal?: AbortSignal): Promise<ListingsResponse> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) {
    if (value !== '' && value !== undefined && value !== null) params.set(key, String(value))
  }
  return request<ListingsResponse>(`/listings?${params.toString()}`, { signal })
}

export function fetchListing(id: number): Promise<Listing> {
  return request<Listing>(`/listings/${id}`)
}

export function refreshListingDetails(id: number, signal?: AbortSignal): Promise<Listing> {
	return request<Listing>(`/listings/${id}/refresh-details`, { method: 'POST', signal })
}

export async function fetchHistory(id: number): Promise<Snapshot[]> {
  const response = await request<{ data: Snapshot[] }>(`/listings/${id}/history`)
  return response.data
}

export function fetchStats(): Promise<Stats> {
  return request<Stats>('/stats')
}

export function requestSourceRefresh(payload: SourceRefreshRequest, signal?: AbortSignal): Promise<SourceRefreshResponse> {
  return request<SourceRefreshResponse>('/source-refresh', { method: 'POST', body: JSON.stringify(payload), signal, headers: { 'Content-Type': 'application/json' } })
}

export function getSourceRefresh(id: string, signal?: AbortSignal): Promise<SourceRefreshResponse> {
  return request<SourceRefreshResponse>(`/source-refresh/${encodeURIComponent(id)}`, { signal })
}

export function resolveImageUrl(value: string): string {
  if (value.startsWith('/')) return `https://www.dccolombia.com.co${value}`
  return value
}
