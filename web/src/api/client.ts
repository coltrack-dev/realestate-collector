import type { ListingFilters, ListingsResponse, Snapshot, Stats, Listing } from '../types'

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

export function fetchListings(filters: ListingFilters): Promise<ListingsResponse> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) {
    if (value !== '' && value !== undefined && value !== null) params.set(key, String(value))
  }
  return request<ListingsResponse>(`/listings?${params.toString()}`)
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

export function resolveImageUrl(value: string): string {
  if (value.startsWith('/')) return `https://www.dccolombia.com.co${value}`
  return value
}
