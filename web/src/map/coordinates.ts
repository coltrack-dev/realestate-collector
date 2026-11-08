import type { Listing } from '../types'

export const COLOMBIA_CENTER: [number, number] = [-74.2973, 4.5709]

export const COLOMBIA_BOUNDS = {
  minLongitude: -82,
  maxLongitude: -66,
  minLatitude: -5,
  maxLatitude: 14,
} as const

export type ListingCoordinate = [longitude: number, latitude: number]

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
  if (
    longitude < COLOMBIA_BOUNDS.minLongitude ||
    longitude > COLOMBIA_BOUNDS.maxLongitude ||
    latitude < COLOMBIA_BOUNDS.minLatitude ||
    latitude > COLOMBIA_BOUNDS.maxLatitude
  ) return null
  return [longitude, latitude]
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
