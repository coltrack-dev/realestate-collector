export interface Listing {
  id: number
  source: string
  external_id: string
  url: string
  title: string
  description: string
  operation: string
  property_type: string
  price: number | null
  currency: string | null
  area_m2: number | null
  bedrooms: number | null
  bathrooms: number | null
  parking_spots: number | null
  country: string | null
  country_code: string | null
  department: string | null
  city: string | null
  neighborhood: string | null
  address: string | null
  latitude: number | null
  longitude: number | null
  resolved_latitude: number | null
  resolved_longitude: number | null
  location_precision: 'EXACT' | 'ADDRESS' | 'NEIGHBORHOOD' | 'CITY' | 'DEPARTMENT' | 'COUNTRY' | 'UNKNOWN' | string
  geocode_provider: string | null
  geocoded_at: string | null
  images: string[]
  published_at: string | null
  collected_at: string
  first_seen_at: string
  last_seen_at: string
  details_fetched_at: string | null
  details_expires_at: string | null
  details_status: 'NOT_LOADED' | 'LOADING' | 'READY' | 'FAILED' | 'UNSUPPORTED' | string
}

export interface Snapshot {
  id: number
  price: number | null
  currency: string | null
  area_m2: number | null
  content_hash: string
  collected_at: string
}

export interface ListingsResponse {
  data: Listing[]
  pagination: { limit: number; offset: number; total: number }
}

export interface Stats {
  total: number
  by_source: Record<string, number>
  by_operation: Record<string, number>
  last_updated_at: string | null
}

export interface ListingFilters {
  source: string
  country_code: string
  city: string
  operation: string
  property_type: string
  min_price: string
  max_price: string
  min_area: string
  max_area: string
  min_longitude: string
  max_longitude: string
  min_latitude: string
  max_latitude: string
  limit: number
  offset: number
}
