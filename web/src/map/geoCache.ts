import type { Listing, ListingFilters } from '../types'

export interface GeoBounds {
  minLongitude: number
  maxLongitude: number
  minLatitude: number
  maxLatitude: number
}

interface CacheEntry {
  key: string
  listings: Listing[]
  covered: GeoBounds
  complete: boolean
  expiresAt: number
  lastUsedAt: number
}

export function expandBounds(bounds: GeoBounds, ratio = 0.25): GeoBounds {
  const longitudePadding = Math.max((bounds.maxLongitude - bounds.minLongitude) * ratio, 0.01)
  const latitudePadding = Math.max((bounds.maxLatitude - bounds.minLatitude) * ratio, 0.01)
  return {
    minLongitude: bounds.minLongitude - longitudePadding,
    maxLongitude: bounds.maxLongitude + longitudePadding,
    minLatitude: Math.max(-90, bounds.minLatitude - latitudePadding),
    maxLatitude: Math.min(90, bounds.maxLatitude + latitudePadding),
  }
}

export function containsBounds(covered: GeoBounds, visible: GeoBounds): boolean {
  return covered.minLongitude <= visible.minLongitude
    && covered.maxLongitude >= visible.maxLongitude
    && covered.minLatitude <= visible.minLatitude
    && covered.maxLatitude >= visible.maxLatitude
}

export function geoCacheKey(filters: ListingFilters): string {
  return JSON.stringify({
    source: filters.source,
    country_code: filters.country_code,
    city: filters.city,
    operation: filters.operation,
    property_type: filters.property_type,
    min_price: filters.min_price,
    max_price: filters.max_price,
    min_area: filters.min_area,
    max_area: filters.max_area,
  })
}

export class GeoResultsCache {
  private readonly entries = new Map<string, CacheEntry>()
  private readonly maxEntries: number
  private readonly ttlMs: number

  constructor(maxEntries = 30, ttlMs = 120_000) {
    this.maxEntries = maxEntries
    this.ttlMs = ttlMs
  }

  get(key: string, visible: GeoBounds, now = Date.now()): Listing[] | null {
    const entry = this.entries.get(key)
    if (!entry || !entry.complete || entry.expiresAt <= now || !containsBounds(entry.covered, visible)) {
      if (entry && entry.expiresAt <= now) this.entries.delete(key)
      return null
    }
    entry.lastUsedAt = now
    return entry.listings
  }

  set(key: string, listings: Listing[], covered: GeoBounds, complete: boolean, now = Date.now()): void {
    this.entries.set(key, { key, listings, covered, complete, expiresAt: now + this.ttlMs, lastUsedAt: now })
    while (this.entries.size > this.maxEntries) {
      const oldest = [...this.entries.values()].sort((left, right) => left.lastUsedAt - right.lastUsedAt)[0]
      if (!oldest) break
      this.entries.delete(oldest.key)
    }
  }

  clear(): void {
    this.entries.clear()
  }

  get size(): number {
    return this.entries.size
  }
}
