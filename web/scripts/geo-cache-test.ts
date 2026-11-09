import assert from 'node:assert/strict'
import { containsBounds, expandBounds, geoCacheKey, GeoResultsCache } from '../src/map/geoCache.ts'
import type { Listing, ListingFilters } from '../src/types.ts'

const bounds = { minLongitude: -74.2, maxLongitude: -74.0, minLatitude: 4.6, maxLatitude: 4.8 }
const expanded = expandBounds(bounds)
assert.equal(containsBounds(expanded, bounds), true)
assert.equal(containsBounds(bounds, expanded), false)

const filters: ListingFilters = {
  source: '', country_code: 'CO', city: 'Bogotá', operation: 'SALE', property_type: 'APARTMENT',
  min_price: '', max_price: '', min_area: '', max_area: '', min_longitude: '', max_longitude: '', min_latitude: '', max_latitude: '', limit: 100, offset: 0,
}
assert.equal(geoCacheKey(filters), geoCacheKey({ ...filters, limit: 50, offset: 200, min_longitude: '-74.2' }))
assert.notEqual(geoCacheKey(filters), geoCacheKey({ ...filters, country_code: 'UY' }))

const cache = new GeoResultsCache(1, 100)
const listing = {} as Listing
cache.set('co', [listing], expanded, true, 0)
assert.deepEqual(cache.get('co', bounds, 99), [listing])
assert.equal(cache.get('co', bounds, 100), null)
cache.set('partial', [listing], expanded, false, 0)
assert.equal(cache.get('partial', bounds, 1), null)
cache.set('co', [listing], expanded, true, 1)
cache.set('uy', [listing], expanded, true, 2)
assert.equal(cache.size, 1)
assert.equal(cache.get('co', bounds, 2), null)
assert.deepEqual(cache.get('uy', bounds, 2), [listing])
console.log('geo cache tests passed')
