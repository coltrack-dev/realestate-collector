import { createServer } from 'node:http'
import { spawn } from 'node:child_process'

const webRoot = new URL('../', import.meta.url).pathname
const mockPort = 8087
const webPort = 5174
let viewportRefreshRequests = []
let plannedSourceQueries = []
let statusRequests = 0

const listing = {
  id: 101, source: 'mock', external_id: 'MOCK-101', url: 'https://example.test/101',
  title: 'Apartamento de prueba', description: '', operation: 'SALE', property_type: 'APARTMENT',
  price: 350000000, currency: 'COP', area_m2: 70, bedrooms: 2, bathrooms: 2, parking_spots: null,
  country: 'Colombia', country_code: 'CO', department: 'Cundinamarca', city: 'Bogotá D.C.', neighborhood: 'Chapinero',
  address: null, latitude: 4.711, longitude: -74.0721, resolved_latitude: null, resolved_longitude: null,
  location_precision: 'EXACT', geocode_provider: null, geocoded_at: null, images: [], published_at: null,
  collected_at: new Date().toISOString(), first_seen_at: new Date().toISOString(), last_seen_at: new Date().toISOString(),
  details_fetched_at: null, details_expires_at: null, details_status: 'NOT_LOADED',
}
const importedListing = { ...listing, id: 202, external_id: 'MOCK-202', title: 'Apartamento incorporado por refresh' }
const inventory = [listing]
let lastViewport = null

const backend = createServer((request, response) => {
  const url = new URL(request.url, `http://127.0.0.1:${mockPort}`)
  response.setHeader('Content-Type', 'application/json')
  if (request.method === 'GET' && url.pathname === '/api/v1/listings') {
    if (url.searchParams.has('min_longitude')) {
      lastViewport = {
        longitude: (Number(url.searchParams.get('min_longitude')) + Number(url.searchParams.get('max_longitude'))) / 2,
        latitude: (Number(url.searchParams.get('min_latitude')) + Number(url.searchParams.get('max_latitude'))) / 2,
      }
    }
    response.end(JSON.stringify({ data: inventory, complete: true, pagination: { limit: 100, offset: 0, total: inventory.length } }))
    return
  }
  if (request.method === 'GET' && url.pathname === '/api/v1/stats') {
    response.end(JSON.stringify({ total: inventory.length, by_source: { mock: inventory.length }, by_operation: { SALE: inventory.length }, last_updated_at: null }))
    return
  }
  if (request.method === 'GET' && url.pathname === '/api/v1/geo/resolve') {
    const longitude = Number(url.searchParams.get('longitude'))
    const latitude = Number(url.searchParams.get('latitude'))
    const places = [
      [-74.1, 4.7, 'Bogotá D.C.'], [-76.53, 3.44, 'Cali'], [-75.58, 6.24, 'Medellín'],
      [-74.8, 10.98, 'Barranquilla'], [-75.5, 10.4, 'Cartagena'], [-75.37, 6.15, 'Rionegro'],
    ]
    const place = places.reduce((best, candidate) => Math.hypot(candidate[0] - longitude, candidate[1] - latitude) < Math.hypot(best[0] - longitude, best[1] - latitude) ? candidate : best)
    const inColombia = longitude >= -79 && longitude <= -66 && latitude >= -5 && latitude <= 14
    const inUruguay = longitude >= -57 && longitude <= -53 && latitude >= -35.5 && latitude <= -30
    response.end(JSON.stringify(inColombia
      ? { country_code: 'CO', department: 'fixture', municipality: place[2], resolution: 'ADMINISTRATIVE_POLYGON', available_sources: ['vixora', 'dccolombia'] }
      : inUruguay
        ? { country_code: 'UY', department: 'Montevideo', municipality: 'Montevideo', resolution: 'ADMINISTRATIVE_POLYGON', available_sources: [] }
        : { resolution: 'UNKNOWN' }))
    return
  }
  if (request.method === 'POST' && url.pathname === '/api/v1/source-refresh/viewport') {
    let body = ''
    request.on('data', (chunk) => { body += chunk })
    request.on('end', () => {
      const payload = JSON.parse(body)
      viewportRefreshRequests.push(payload)
      if (payload.country_code === 'UY') {
        response.statusCode = 202
        response.end(JSON.stringify({ queries: [], skipped: [{ area: { country_code: 'UY' }, reason: 'no_permitted_source' }] }))
        return
      }
      if (lastViewport) {
        importedListing.longitude = lastViewport.longitude
        importedListing.latitude = lastViewport.latitude
      }
      if (!inventory.some((item) => item.id === importedListing.id)) inventory.push(importedListing)
      response.statusCode = 202
      const first = payload.country_code === 'CO' ? 'Bogotá D.C.' : 'Montevideo'
      const second = payload.country_code === 'CO' ? 'Soacha' : 'Canelones'
      const plan = [first, second].map((municipality, index) => {
        const refreshId = `mock-${plannedSourceQueries.length + index + 1}`
        plannedSourceQueries.push({ refreshId, municipality, payload })
        return { area: { country_code: payload.country_code, municipality, overlap_area_m2: 100 - index, overlap_ratio: 0.5 - index / 10 }, result: { status: 'QUEUED', refresh_id: refreshId } }
      })
      response.end(JSON.stringify({ queries: plan, skipped: [] }))
    })
    return
  }
  if (request.method === 'GET' && url.pathname.startsWith('/api/v1/source-refresh/')) {
    statusRequests++
    response.end(JSON.stringify({ status: 'DONE', refresh_id: url.pathname.split('/').pop() }))
    return
  }
  response.statusCode = 404
  response.end(JSON.stringify({ error: 'not found' }))
})

async function waitFor(condition, timeout = 15000) {
  const started = Date.now()
  return new Promise((resolve, reject) => {
    const poll = async () => {
      if (await condition()) return resolve()
      if (Date.now() - started > timeout) return reject(new Error('browser test timeout'))
      setTimeout(poll, 100)
    }
    poll()
  })
}

async function webdriver(method, path, body) {
  const response = await fetch(`http://127.0.0.1:4444${path}`, {
    method, headers: { 'content-type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body),
  })
  const payload = await response.json()
  if (!response.ok || payload.value?.error) throw new Error(JSON.stringify(payload))
  return payload.value
}

async function clickSelector(selector) {
  const element = await webdriver('POST', `/session/${session}/element`, { using: 'css selector', value: selector })
  await webdriver('POST', `/session/${session}/element/${element['element-6066-11e4-a52e-4f735466cecf']}/click`, {})
}

let vite
let geckodriver
let session
try {
  await new Promise((resolve) => backend.listen(mockPort, '127.0.0.1', resolve))
  vite = spawn('npx', ['vite', '--config', 'vite.config.ts', '--host', '127.0.0.1', '--port', String(webPort), '--strictPort'], { cwd: webRoot, stdio: 'ignore', env: { ...process.env, VITE_API_PROXY: `http://127.0.0.1:${mockPort}` } })
  geckodriver = spawn('geckodriver', ['--port', '4444', '--log', 'fatal'], { stdio: 'ignore' })
  await waitFor(async () => {
    try { await fetch(`http://127.0.0.1:${webPort}`); return true } catch { return false }
  })
  await waitFor(async () => {
    try { await fetch('http://127.0.0.1:4444/status'); return true } catch { return false }
  })
  const createdSession = await webdriver('POST', '/session', { capabilities: { alwaysMatch: { browserName: 'firefox', 'moz:firefoxOptions': { args: ['-headless'] } } } })
  session = createdSession.sessionId
  console.log('browser session created')
  await webdriver('POST', `/session/${session}/url`, { url: `http://127.0.0.1:${webPort}` })
  console.log('page loaded')
  await webdriver('POST', `/session/${session}/execute/sync`, { script: `
    const mapButton = [...document.querySelectorAll('button')].find((button) => button.textContent.trim() === 'Mapa');
    mapButton.click();
    return true;
  `, args: [] })
  console.log('map tab selected')
  await waitFor(() => viewportRefreshRequests.length === 0)
  await webdriver('POST', `/session/${session}/execute/sync`, { script: `
    const set = (selector, value) => { const element = document.querySelector(selector); element.value = value; element.dispatchEvent(new Event('change', { bubbles: true })); };
    set('select[aria-label="País"]', 'CO');
    set('select[aria-label="Operación"]', 'SALE');
    set('select[aria-label="Tipo de propiedad"]', 'APARTMENT');
    return true;
  `, args: [] })
  await new Promise((resolve) => setTimeout(resolve, 250))
  await webdriver('POST', `/session/${session}/execute/sync`, { script: `[...document.querySelectorAll('button')].find((button) => button.textContent.trim() === 'Aplicar').click(); return true;`, args: [] })
  console.log('map filters applied')
  const filterValues = await webdriver('POST', `/session/${session}/execute/sync`, { script: `return [...document.querySelectorAll('select[aria-label]')].map((element) => element.value)`, args: [] })
  console.log('filter values', JSON.stringify(filterValues))
  const geoChecks = await webdriver('POST', `/session/${session}/execute/sync`, { script: `
    const points = [[-74.07,4.71],[-76.53,3.44],[-75.58,6.24],[-75.50,10.40],[-74.80,10.98],[-75.37,6.15],[-56.16,-34.90],[-40,0]];
    return Promise.all(points.map(([longitude, latitude]) => fetch('/api/v1/geo/resolve?longitude=' + longitude + '&latitude=' + latitude).then((response) => response.json())));
  `, args: [] })
  const expectedCities = ['Bogotá D.C.', 'Cali', 'Medellín', 'Cartagena', 'Barranquilla', 'Rionegro']
  if (geoChecks.slice(0, 6).some((result, index) => result.municipality !== expectedCities[index]) || geoChecks[6].country_code !== 'UY' || geoChecks[7].resolution !== 'UNKNOWN') {
    throw new Error(`unexpected geographic resolutions: ${JSON.stringify(geoChecks)}`)
  }
  await waitFor(async () => {
    const ready = await webdriver('POST', `/session/${session}/execute/sync`, { script: `return document.querySelector('.maplibregl-ctrl-zoom-in') !== null`, args: [] })
    return ready === true
  })
  console.log('map zoom control ready')
  for (let index = 0; index < 45; index += 1) await clickSelector('.maplibregl-ctrl-zoom-in')
  console.log('map moved')
  await new Promise((resolve) => setTimeout(resolve, 5000))
  const diagnostics = await webdriver('POST', `/session/${session}/execute/sync`, { script: `return window.__mapRefreshDiagnostics ?? []`, args: [] })
  console.log('diagnostic refresh events', diagnostics.filter((entry) => entry.event.includes('refresh')).map((entry) => entry.details))
  await waitFor(() => viewportRefreshRequests.length >= 1)
  const payload = viewportRefreshRequests[0]
  if (payload.country_code !== 'CO' || payload.operation !== 'SALE' || payload.property_type !== 'APARTMENT' || payload.max_queries !== 3) {
    throw new Error(`unexpected refresh payload: ${JSON.stringify(payload)}`)
  }
  if (plannedSourceQueries.filter((query) => query.payload === payload).length < 2) throw new Error('viewport planner did not create multiple municipality queries')
  await waitFor(async () => {
    const bodyText = await webdriver('POST', `/session/${session}/execute/sync`, { script: `return document.body.textContent.includes('Apartamento incorporado por refresh')`, args: [] })
    return bodyText === true
  })
  if (!diagnostics.some((entry) => entry.event === 'map_moveend') || !diagnostics.some((entry) => entry.event === 'map_refresh_post_started')) {
    throw new Error(`missing map refresh diagnostics: ${JSON.stringify(diagnostics)}`)
  }
  const beforeAllFilters = viewportRefreshRequests.length
  await webdriver('POST', `/session/${session}/execute/sync`, { script: `
    const set = (selector, value) => { const element = document.querySelector(selector); element.value = value; element.dispatchEvent(new Event('change', { bubbles: true })); };
    set('select[aria-label="Operación"]', '');
    set('select[aria-label="Tipo de propiedad"]', '');
    [...document.querySelectorAll('button')].find((button) => button.textContent.trim() === 'Aplicar').click();
    return true;
  `, args: [] })
  for (let index = 0; index < 3; index += 1) await clickSelector('.maplibregl-ctrl-zoom-in')
  await waitFor(() => viewportRefreshRequests.length > beforeAllFilters, 10000)
  const allPayload = viewportRefreshRequests[viewportRefreshRequests.length - 1]
  if (allPayload.country_code !== 'CO' || allPayload.operation !== '' || allPayload.property_type !== '' || allPayload.max_queries !== 3) {
    throw new Error(`unexpected ALL refresh payload: ${JSON.stringify(allPayload)}`)
  }
  const uruguayResult = await webdriver('POST', `/session/${session}/execute/async`, { script: `
    const done = arguments[arguments.length - 1];
    fetch('/api/v1/source-refresh/viewport', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ country_code: 'UY', operation: '', property_type: '', max_queries: 3, bbox: { min_longitude: -56.3, max_longitude: -55.8, min_latitude: -35.1, max_latitude: -34.7 } }) })
      .then((response) => response.json().then((body) => ({ status: response.status, body }))).then(done);
  `, args: [] })
  const uruguayPayload = viewportRefreshRequests[viewportRefreshRequests.length - 1]
  if (uruguayPayload.country_code !== 'UY' || uruguayResult.status !== 202 || uruguayResult.body.queries.length !== 0) throw new Error(`unexpected Uruguay refresh result: ${JSON.stringify({ uruguayPayload, uruguayResult })}`)
  if (plannedSourceQueries.some((query) => query.payload === uruguayPayload)) throw new Error('disabled Uruguay source produced a planned query')
  console.log(JSON.stringify({ browser: 'firefox', viewportRefreshPosts: viewportRefreshRequests.length, plannedSourceQueries: plannedSourceQueries.length, statusPolls: statusRequests, payload, allPayload, uruguayPayload }))
} finally {
  if (session) await webdriver('DELETE', `/session/${session}`)
  geckodriver?.kill('SIGTERM')
  vite?.kill('SIGTERM')
  backend.close()
}
