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
  department: string | null
  city: string | null
  neighborhood: string | null
  address: string | null
  latitude: number | null
  longitude: number | null
  images: string[]
  published_at: string | null
  collected_at: string
  first_seen_at: string
  last_seen_at: string
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
  city: string
  operation: string
  property_type: string
  min_price: string
  max_price: string
  min_area: string
  max_area: string
  limit: number
  offset: number
}
