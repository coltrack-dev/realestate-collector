import type { Listing } from '../types'

export const COLOMBIA_CENTER: [number, number] = [-74.2973, 4.5709]
export const URUGUAY_CENTER: [number, number] = [-56.1645, -32.5228]

export const COLOMBIA_BOUNDS = {
  minLongitude: -82,
  maxLongitude: -66,
  minLatitude: -5,
  maxLatitude: 14,
} as const
export const URUGUAY_BOUNDS = {
  minLongitude: -59,
  maxLongitude: -53,
  minLatitude: -35.5,
  maxLatitude: -30,
} as const

export type ListingCoordinate = [longitude: number, latitude: number]
export type LocationPrecision = 'EXACT' | 'ADDRESS' | 'NEIGHBORHOOD' | 'CITY' | 'DEPARTMENT' | 'COUNTRY' | 'UNKNOWN'

export interface NavigationTarget {
  coordinate: ListingCoordinate
  precision: LocationPrecision
}

export interface CoordinateDiagnostics {
  withCoordinates: number
  withoutCoordinates: number
  invalidCoordinates: number
}

export interface CoordinateExtent {
  minLongitude: number
  minLatitude: number
  maxLongitude: number
  maxLatitude: number
}

function isFiniteNumber(value: number | null): value is number {
  return value != null && Number.isFinite(value)
}

export function getValidCoordinates(listing: Listing): ListingCoordinate | null {
  const { latitude, longitude } = listing
  if (!isFiniteNumber(latitude) || !isFiniteNumber(longitude)) return null
  if (latitude === 0 && longitude === 0) return null
  const bounds = listing.country_code === 'UY' ? URUGUAY_BOUNDS : COLOMBIA_BOUNDS
  if (longitude < bounds.minLongitude || longitude > bounds.maxLongitude || latitude < bounds.minLatitude || latitude > bounds.maxLatitude) return null
  return [longitude, latitude]
}

function countryCode(listing: Listing): string {
  if (listing.country_code) return listing.country_code.toUpperCase()
  if ((listing.country || '').toLowerCase().includes('uruguay')) return 'UY'
  if ((listing.country || '').toLowerCase().includes('colombia')) return 'CO'
  return ''
}

function countryCenter(code: string): ListingCoordinate | null {
  if (code === 'CO') return COLOMBIA_CENTER
  if (code === 'UY') return URUGUAY_CENTER
  return null
}

function cityCenter(listing: Listing): ListingCoordinate | null {
  const city = (listing.city || '').trim().toLowerCase()
  const code = countryCode(listing)
  if (code === 'CO') {
    if (city.includes('bogot')) return [-74.0721, 4.7110]
    if (city.includes('medell')) return [-75.5812, 6.2442]
    if (city === 'rionegro') return [-75.3740, 6.1550]
  }
  if (code === 'UY') {
    if (city === 'montevideo') return [-56.1645, -34.9011]
    if (city === 'maldonado') return [-54.9500, -34.9000]
    if (city === 'punta del este') return [-54.9333, -34.9500]
  }
  return null
}

function departmentCenter(listing: Listing): ListingCoordinate | null {
  const department = (listing.department || '').trim().toLowerCase()
  const code = countryCode(listing)
  if (code === 'CO' && department === 'antioquia') return [-75.5636, 6.2518]
  if (code === 'CO' && department === 'cundinamarca') return [-74.2647, 4.7318]
  if (code === 'UY' && department === 'montevideo') return [-56.1645, -34.9011]
  if (code === 'UY' && department === 'maldonado') return [-54.9500, -34.9000]
  return null
}

function isValidResolvedCoordinate(listing: Listing): ListingCoordinate | null {
  if (!isFiniteNumber(listing.resolved_latitude) || !isFiniteNumber(listing.resolved_longitude)) return null
  if (listing.resolved_latitude === 0 && listing.resolved_longitude === 0) return null
  const bounds = countryCode(listing) === 'UY' ? URUGUAY_BOUNDS : COLOMBIA_BOUNDS
  if (listing.resolved_longitude < bounds.minLongitude || listing.resolved_longitude > bounds.maxLongitude || listing.resolved_latitude < bounds.minLatitude || listing.resolved_latitude > bounds.maxLatitude) return null
  return [listing.resolved_longitude, listing.resolved_latitude]
}

export function getLocationPrecision(listing: Listing): LocationPrecision {
  if (getValidCoordinates(listing)) return 'EXACT'
  const declared = listing.location_precision as LocationPrecision
  if (isValidResolvedCoordinate(listing) && declared !== 'UNKNOWN' && declared) return declared
  if (cityCenter(listing)) return 'CITY'
  if (departmentCenter(listing)) return 'DEPARTMENT'
  if (countryCenter(countryCode(listing))) return 'COUNTRY'
  return 'UNKNOWN'
}

export function getNavigationTarget(listing: Listing): NavigationTarget | null {
  const exact = getValidCoordinates(listing)
  if (exact) return { coordinate: exact, precision: 'EXACT' }
  const resolved = isValidResolvedCoordinate(listing)
  if (resolved && listing.location_precision && listing.location_precision !== 'UNKNOWN') {
    return { coordinate: resolved, precision: listing.location_precision as LocationPrecision }
  }
  const city = cityCenter(listing)
  if (city) return { coordinate: city, precision: 'CITY' }
  const department = departmentCenter(listing)
  if (department) return { coordinate: department, precision: 'DEPARTMENT' }
  const country = countryCenter(countryCode(listing))
  if (country) return { coordinate: country, precision: 'COUNTRY' }
  return null
}

function hasCoordinateValues(listing: Listing): boolean {
  return listing.latitude != null || listing.longitude != null
}

export function getValidListings(listings: Listing[]): Listing[] {
  return listings.filter((listing) => getValidCoordinates(listing) !== null)
}

export function getCoordinateDiagnostics(listings: Listing[]): CoordinateDiagnostics {
  let withCoordinates = 0
  let withoutCoordinates = 0
  let invalidCoordinates = 0

  for (const listing of listings) {
    if (getValidCoordinates(listing)) withCoordinates += 1
    else if (hasCoordinateValues(listing)) invalidCoordinates += 1
    else withoutCoordinates += 1
  }

  return { withCoordinates, withoutCoordinates, invalidCoordinates }
}

export function getCoordinateExtent(listings: Listing[]): CoordinateExtent | null {
  const coordinates = listings
    .map(getValidCoordinates)
    .filter((coordinate): coordinate is ListingCoordinate => coordinate !== null)
  if (!coordinates.length) return null

  return coordinates.reduce<CoordinateExtent>((extent, [longitude, latitude]) => ({
    minLongitude: Math.min(extent.minLongitude, longitude),
    minLatitude: Math.min(extent.minLatitude, latitude),
    maxLongitude: Math.max(extent.maxLongitude, longitude),
    maxLatitude: Math.max(extent.maxLatitude, latitude),
  }), {
    minLongitude: coordinates[0][0],
    minLatitude: coordinates[0][1],
    maxLongitude: coordinates[0][0],
    maxLatitude: coordinates[0][1],
  })
}
