<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import FilterBar from './components/FilterBar.vue'
import ListingCard from './components/ListingCard.vue'
import ListingMap from './components/ListingMap.vue'
import DetailDrawer from './components/DetailDrawer.vue'
import SiteHeader from './components/SiteHeader.vue'
import { fetchListings, fetchStats } from './api/client'
import type { Listing, ListingFilters, Stats } from './types'
import { getValidCoordinates } from './map/coordinates'
import { expandBounds, geoCacheKey, GeoResultsCache, type GeoBounds } from './map/geoCache'

const catalogListings = ref<Listing[]>([])
const viewportListings = ref<Listing[]>([])
const stats = ref<Stats | null>(null)
const selectedListingId = ref<number | null>(null)
const selectedListingSnapshot = ref<Listing | null>(null)
const filters = ref<ListingFilters>({ source: '', country_code: '', city: '', operation: '', property_type: '', min_price: '', max_price: '', min_area: '', max_area: '', min_longitude: '', max_longitude: '', min_latitude: '', max_latitude: '', limit: 50, offset: 0 })
const loading = ref(true)
const error = ref('')
const hasMore = ref(false)
const listingScroll = ref<HTMLElement | null>(null)
const mapListingScroll = ref<HTMLElement | null>(null)
const mobileView = ref<'list' | 'map'>('list')
const activeTab = ref<'properties' | 'map' | 'stats'>('properties')
const mapListOpen = ref(true)
const mapViewport = ref<GeoBounds | null>(null)
const viewportLoading = ref(false)
const viewportError = ref('')
const viewportHasLoaded = ref(false)
let listingsController: AbortController | null = null
let mapController: AbortController | null = null
let mapRequestSequence = 0
let mapDebounceTimer: ReturnType<typeof setTimeout> | null = null
const geoCache = new GeoResultsCache()
const mapDraft = reactive({ country_code: '', operation: '', property_type: '', city: '', min_price: '', max_price: '' })

const selected = computed(() => catalogListings.value.find((listing) => listing.id === selectedListingId.value)
  ?? viewportListings.value.find((listing) => listing.id === selectedListingId.value)
  ?? selectedListingSnapshot.value)
const locatedCount = computed(() => catalogListings.value.filter((listing) => getValidCoordinates(listing) !== null).length)
const totalLabel = computed(() => stats.value?.total ?? 0)
const countryLabel = computed(() => filters.value.country_code === 'UY' ? 'Uruguay' : filters.value.country_code === 'CO' ? 'Colombia' : 'Colombia y Uruguay')

async function loadListings(nextFilters = filters.value) {
  if (mapDebounceTimer) {
    clearTimeout(mapDebounceTimer)
    mapDebounceTimer = null
  }
  mapController?.abort()
  mapRequestSequence++
  listingsController?.abort()
  const controller = new AbortController()
  listingsController = controller
  loading.value = true; error.value = ''
  try {
    const response = await fetchListings(nextFilters, controller.signal)
    catalogListings.value = response.data
    if (activeTab.value === 'map' && !viewportHasLoaded.value) {
      viewportListings.value = [...response.data]
      viewportHasLoaded.value = true
    }
    filters.value = { ...nextFilters, offset: response.pagination.offset }
    hasMore.value = response.pagination.offset + response.data.length < response.pagination.total
  } catch (caught) {
    if (!(caught instanceof DOMException && caught.name === 'AbortError')) error.value = caught instanceof Error ? caught.message : 'No se pudieron cargar los inmuebles'
  } finally { if (listingsController === controller) loading.value = false }
}

async function loadStats() { try { stats.value = await fetchStats() } catch { /* Catalog remains useful when stats are unavailable. */ } }
function applyFilters(next: ListingFilters) {
  viewportError.value = ''
  void loadListings(next)
}
function resetFilters() { const next = { ...filters.value, source: '', country_code: '', city: '', operation: '', property_type: '', min_price: '', max_price: '', min_area: '', max_area: '', min_longitude: '', max_longitude: '', min_latitude: '', max_latitude: '', offset: 0 }; applyFilters(next) }
async function selectListing(listing: Listing, source: 'marker-click' | 'listing-click') {
  selectedListingId.value = listing.id
  selectedListingSnapshot.value = listing
  if (source !== 'marker-click') return
  await nextTick()
  const container = activeTab.value === 'map' ? mapListingScroll.value : listingScroll.value
  const card = container?.querySelector<HTMLElement>(`[data-listing-id="${listing.id}"]`)
  if (!container || !card) return
  const top = card.offsetTop - Math.max(0, (container.clientHeight - card.offsetHeight) / 2)
  container.scrollTo({ top: Math.max(0, top), behavior: 'smooth' })
}
function selectListingFromCard(listing: Listing) { void selectListing(listing, 'listing-click') }
function selectListingFromMarker(listing: Listing) { void selectListing(listing, 'marker-click') }
function closeDetail() { selectedListingId.value = null }
function movePage(direction: number) { void loadListings({ ...filters.value, offset: Math.max(0, filters.value.offset + direction * filters.value.limit) }) }
function selectTab(tab: 'properties' | 'map' | 'stats') {
  activeTab.value = tab
  if (tab === 'map') mobileView.value = 'map'
  if (tab === 'properties') mobileView.value = 'list'
  if (tab === 'map') {
    syncMapDraft()
    if (!viewportHasLoaded.value && !loading.value) {
      viewportListings.value = [...catalogListings.value]
      viewportHasLoaded.value = true
    }
  }
}
function syncMapDraft() {
  mapDraft.country_code = filters.value.country_code
  mapDraft.operation = filters.value.operation
  mapDraft.property_type = filters.value.property_type
  mapDraft.city = filters.value.city
  mapDraft.min_price = filters.value.min_price
  mapDraft.max_price = filters.value.max_price
}
function applyMapFilters() {
  const next = { ...filters.value, ...mapDraft, min_longitude: '', max_longitude: '', min_latitude: '', max_latitude: '', offset: 0 }
  filters.value = next
  viewportError.value = ''
  if (mapViewport.value) scheduleMapSearch(mapViewport.value)
  else void loadListings(next)
}

function filterListingsToViewport(values: Listing[], bounds: GeoBounds): Listing[] {
  return values.filter((listing) => {
    const coordinate = getValidCoordinates(listing)
    return coordinate !== null
      && coordinate[0] >= bounds.minLongitude && coordinate[0] <= bounds.maxLongitude
      && coordinate[1] >= bounds.minLatitude && coordinate[1] <= bounds.maxLatitude
  })
}

function preserveSelection(values: Listing[]): void {
  const current = values.find((listing) => listing.id === selectedListingId.value)
  if (current) selectedListingSnapshot.value = current
}

function scheduleMapSearch(bounds: GeoBounds): void {
  mapViewport.value = bounds
  mapController?.abort()
  mapController = null
  mapRequestSequence++
  if (mapDebounceTimer) clearTimeout(mapDebounceTimer)
  mapDebounceTimer = setTimeout(() => {
    mapDebounceTimer = null
    void loadMapArea(bounds)
  }, 350)
}

async function loadMapArea(bounds: GeoBounds): Promise<void> {
  const expanded = expandBounds(bounds)
  const requestFilters: ListingFilters = {
    ...filters.value,
    min_longitude: String(expanded.minLongitude),
    max_longitude: String(expanded.maxLongitude),
    min_latitude: String(expanded.minLatitude),
    max_latitude: String(expanded.maxLatitude),
    limit: 100,
    offset: 0,
  }
  const key = geoCacheKey(requestFilters)
  const cached = geoCache.get(key, bounds)
  if (cached) {
    viewportListings.value = filterListingsToViewport(cached, bounds)
    preserveSelection(viewportListings.value)
    viewportHasLoaded.value = true
    viewportLoading.value = false
    viewportError.value = ''
    return
  }

  mapController?.abort()
  const controller = new AbortController()
  mapController = controller
  const requestSequence = ++mapRequestSequence
  viewportLoading.value = true
  viewportError.value = ''
  try {
    const response = await fetchListings(requestFilters, controller.signal)
    if (requestSequence !== mapRequestSequence) return
    geoCache.set(key, response.data, expanded, response.complete, Date.now())
    viewportListings.value = filterListingsToViewport(response.data, bounds)
    preserveSelection(viewportListings.value)
    viewportHasLoaded.value = true
  } catch (caught) {
    if (requestSequence !== mapRequestSequence || (caught instanceof DOMException && caught.name === 'AbortError')) return
    viewportError.value = caught instanceof Error ? caught.message : 'No se pudo actualizar esta zona'
  } finally {
    if (requestSequence === mapRequestSequence) viewportLoading.value = false
  }
}

function handleViewportChange(bounds: GeoBounds) {
  scheduleMapSearch(bounds)
}

onMounted(() => { syncMapDraft(); void loadListings(); void loadStats() })
onBeforeUnmount(() => {
  if (mapDebounceTimer) clearTimeout(mapDebounceTimer)
  mapController?.abort()
  listingsController?.abort()
})
</script>

<template>
  <div class="app-shell">
    <SiteHeader :total="totalLabel" :active-tab="activeTab" @navigate="selectTab" />

    <main class="workspace" :class="[`mobile-view-${mobileView}`, `active-tab-${activeTab}`]">
      <div class="mobile-view-toggle" role="tablist" aria-label="Cambiar vista">
        <button type="button" :class="{ active: activeTab === 'properties' }" role="tab" :aria-selected="activeTab === 'properties'" @click="selectTab('properties')">Propiedades</button>
        <button type="button" :class="{ active: activeTab === 'map' }" role="tab" :aria-selected="activeTab === 'map'" @click="selectTab('map')">Mapa</button>
      </div>
      <section id="catalogo" class="catalog-column" :class="{ 'is-hidden': activeTab === 'map' }">
        <div v-if="activeTab === 'stats'" class="stats-view">
          <span class="eyebrow">Lectura del mercado</span>
          <h1>Estadísticas del inventario</h1>
          <div class="stats-grid">
            <div class="stats-card"><small>Total de propiedades</small><strong>{{ stats?.total ?? catalogListings.length }}</strong></div>
            <div class="stats-card"><small>Última actualización</small><strong>{{ stats?.last_updated_at ? new Date(stats.last_updated_at).toLocaleDateString('es-CO') : '—' }}</strong></div>
          </div>
          <div class="stats-columns">
            <section><h2>Por fuente</h2><p v-for="(count, source) in stats?.by_source" :key="source"><span>{{ source }}</span><strong>{{ count }}</strong></p></section>
            <section><h2>Por operación</h2><p v-for="(count, operation) in stats?.by_operation" :key="operation"><span>{{ operation === 'SALE' ? 'Venta' : operation === 'RENT' ? 'Arriendo' : operation }}</span><strong>{{ count }}</strong></p></section>
          </div>
        </div>
        <div class="catalog-header"><div><span class="eyebrow">Inventario de {{ countryLabel }}</span><h1>Propiedades para <em>explorar</em></h1></div><div id="estadisticas" class="catalog-summary"><strong>{{ catalogListings.length }}</strong><span>resultados<br />en esta vista</span></div></div>
        <FilterBar @apply="applyFilters" @reset="resetFilters" />
        <div class="active-filters"><span v-if="filters.country_code" class="filter-chip">{{ filters.country_code === 'UY' ? 'Uruguay' : 'Colombia' }} <button type="button" @click="applyFilters({ ...filters, country_code: '' })">×</button></span><span v-if="filters.city" class="filter-chip">{{ filters.city }} <button type="button" @click="applyFilters({ ...filters, city: '' })">×</button></span><span v-if="filters.operation" class="filter-chip">{{ filters.operation === 'SALE' ? 'Venta' : 'Arriendo' }} <button type="button" @click="applyFilters({ ...filters, operation: '' })">×</button></span><span v-if="filters.property_type" class="filter-chip">{{ filters.property_type }} <button type="button" @click="applyFilters({ ...filters, property_type: '' })">×</button></span></div>
        <div v-if="loading" class="state-panel"><div class="loader"></div><strong>Buscando propiedades</strong><span>Consultando el inventario real…</span></div>
        <div v-else-if="error" class="state-panel error-state"><span class="state-icon">!</span><strong>No pudimos cargar el catálogo</strong><span>{{ error }}</span><button class="secondary-button" type="button" @click="loadListings()">Reintentar</button></div>
        <div v-else-if="!catalogListings.length" class="state-panel"><span class="state-icon">⌂</span><strong>No encontramos resultados</strong><span>Ajusta los filtros para ampliar tu búsqueda.</span></div>
        <template v-else>
          <div ref="listingScroll" class="listing-grid"><ListingCard v-for="listing in catalogListings" :key="listing.id" :listing="listing" :selected="selected?.id === listing.id" :data-listing-id="listing.id" @select="selectListingFromCard" /></div>
          <div class="pagination"><button type="button" :disabled="filters.offset === 0" @click="movePage(-1)">← Anterior</button><span>Página {{ Math.floor(filters.offset / filters.limit) + 1 }}</span><button type="button" :disabled="!hasMore" @click="movePage(1)">Siguiente →</button></div>
        </template>
      </section>
      <aside id="mapa" class="map-column" :class="{ 'is-map-tab': activeTab === 'map' }">
        <div v-if="activeTab !== 'map'" class="map-heading"><div><span class="eyebrow">Vista geográfica</span><h2>Descubre por ubicación</h2></div><span class="map-count"><b>{{ locatedCount }}</b> con coordenadas</span></div>
        <div v-else class="map-explorer-toolbar">
          <div class="map-toolbar-title"><span class="eyebrow">Exploración geográfica</span><strong>{{ viewportListings.length }} propiedades visibles</strong></div>
          <div class="map-toolbar-filters">
            <select v-model="mapDraft.country_code" aria-label="País"><option value="">Todos los países</option><option value="CO">Colombia</option><option value="UY">Uruguay</option></select>
            <select v-model="mapDraft.operation" aria-label="Operación"><option value="">Venta y arriendo</option><option value="SALE">Venta</option><option value="RENT">Arriendo</option></select>
            <select v-model="mapDraft.property_type" aria-label="Tipo de propiedad"><option value="">Todos los tipos</option><option value="APARTMENT">Apartamento</option><option value="HOUSE">Casa</option><option value="LAND">Lote</option><option value="COMMERCIAL">Comercial</option></select>
            <input v-model="mapDraft.city" type="text" placeholder="Ciudad o región" aria-label="Ciudad o región" />
            <input v-model="mapDraft.min_price" type="number" min="0" placeholder="Precio mínimo" aria-label="Precio mínimo" />
            <input v-model="mapDraft.max_price" type="number" min="0" placeholder="Precio máximo" aria-label="Precio máximo" />
            <button type="button" class="map-filter-button" @click="applyMapFilters">Aplicar</button>
          </div>
        </div>
        <div class="map-stage">
          <ListingMap :listings="viewportListings" :selected-id="selected?.id ?? null" @select="selectListingFromMarker" @viewport-change="handleViewportChange" />
          <div v-if="viewportLoading" class="map-auto-status" role="status">Actualizando propiedades…</div>
          <div v-else-if="viewportError" class="map-auto-status map-search-error" role="status">No se pudieron actualizar las propiedades.</div>
          <aside v-if="activeTab === 'map'" class="map-list-panel" :class="{ open: mapListOpen }" aria-label="Propiedades en el mapa">
            <button type="button" class="map-list-toggle" @click="mapListOpen = !mapListOpen">{{ mapListOpen ? 'Ocultar lista' : 'Ver propiedades' }} <span aria-hidden="true">{{ mapListOpen ? '←' : '→' }}</span></button>
            <div v-show="mapListOpen" ref="mapListingScroll" class="map-list-scroll">
              <ListingCard v-for="listing in viewportListings" :key="`map-${listing.id}`" :listing="listing" :selected="selected?.id === listing.id" :data-listing-id="listing.id" @select="selectListingFromCard" />
              <p v-if="viewportHasLoaded && !viewportLoading && !viewportListings.length" class="map-list-empty">No hay propiedades en esta zona. Mueve el mapa para explorar otras ubicaciones.</p>
            </div>
          </aside>
        </div>
      </aside>
    </main>
    <DetailDrawer :listing="selected" @close="closeDetail" />
  </div>
</template>
