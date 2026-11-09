import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const app = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const map = readFileSync(new URL('../src/components/ListingMap.vue', import.meta.url), 'utf8')
const styles = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')

assert.match(app, /<ListingMap :listings="viewportListings"/)
assert.match(app, /viewportHasLoaded && !viewportLoading && !viewportListings\.length/)
assert.doesNotMatch(map, /class="map-empty"/)
assert.match(styles, /\.map-auto-status[^}]*pointer-events:\s*none/)

console.log('map empty-state regression checks passed')
