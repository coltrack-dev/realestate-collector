import assert from 'node:assert/strict'
import { getCoordinateDiagnostics, getCoordinateExtent, getValidCoordinates } from '../src/map/coordinates.ts'
import type { Listing } from '../src/types.ts'

function listing(latitude: number | null, longitude: number | null): Listing {
  return {
    id: 1, source: 'test', external_id: 'test-1', url: '', title: 'Test', description: '',
    operation: 'SALE', property_type: 'APARTMENT', price: null, currency: null, area_m2: null,
    bedrooms: null, bathrooms: null, parking_spots: null, country: 'Colombia', department: null,
    city: 'Bogotá', neighborhood: null, address: null, latitude, longitude, images: [],
    published_at: null, collected_at: '', first_seen_at: '', last_seen_at: '',
  }
}

assert.deepEqual(getValidCoordinates(listing(4.65, -74.08)), [-74.08, 4.65])
assert.equal(getValidCoordinates(listing(null, -74.08)), null)
assert.equal(getValidCoordinates(listing(0, 0)), null)
assert.equal(getValidCoordinates(listing(6.292, 75.275)), null)

const diagnostics = getCoordinateDiagnostics([
  listing(4.65, -74.08), listing(null, null), listing(6.292, 75.275), listing(0, 0),
])
assert.deepEqual(diagnostics, { withCoordinates: 1, withoutCoordinates: 1, invalidCoordinates: 2 })
assert.deepEqual(getCoordinateExtent([listing(4.65, -74.08), listing(6.2, -75.3)]), {
  minLongitude: -75.3, minLatitude: 4.65, maxLongitude: -74.08, maxLatitude: 6.2,
})
assert.equal(getCoordinateExtent([listing(null, null)]), null)
console.log('coordinate tests passed')
