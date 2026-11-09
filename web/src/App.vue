<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import FilterBar from './components/FilterBar.vue'
import ListingCard from './components/ListingCard.vue'
import ListingMap from './components/ListingMap.vue'
import DetailDrawer from './components/DetailDrawer.vue'
import SiteHeader from './components/SiteHeader.vue'
import { fetchListings, fetchStats } from './api/client'
import type { Listing, ListingFilters, Stats } from './types'
import { getValidCoordinates } from './map/coordinates'

const listings = ref<Listing[]>([])
const stats = ref<Stats | null>(null)
const selectedListingId = ref<number | null>(null)
const filters = ref<ListingFilters>({ source: '', country_code: '', city: '', operation: '', property_type: '', min_price: '', max_price: '', min_area: '', max_area: '', min_longitude: '', max_longitude: '', min_latitude: '', max_latitude: '', limit: 50, offset: 0 })
const loading = ref(true)
const error = ref('')
const hasMore = ref(false)
const listingScroll = ref<HTMLElement | null>(null)
const mapListingScroll = ref<HTMLElement | null>(null)
const mobileView = ref<'list' | 'map'>('list')
const activeTab = ref<'properties' | 'map' | 'stats'>('properties')
const mapListOpen = ref(true)
const mapViewport = ref<{ minLongitude: number; maxLongitude: number; minLatitude: number; maxLatitude: number } | null>(null)
const mapSearchAvailable = ref(false)
let listingsController: AbortController | null = null
const mapDraft = reactive({ country_code: '', operation: '', property_type: '', city: '', min_price: '', max_price: '' })

const selected = computed(() => listings.value.find((listing) => listing.id === selectedListingId.value) ?? null)
const locatedCount = computed(() => listings.value.filter((listing) => getValidCoordinates(listing) !== null).length)
const totalLabel = computed(() => stats.value?.total ?? 0)
const countryLabel = computed(() => filters.value.country_code === 'UY' ? 'Uruguay' : filters.value.country_code === 'CO' ? 'Colombia' : 'Colombia y Uruguay')

async function loadListings(nextFilters = filters.value) {
  listingsController?.abort()
  const controller = new AbortController()
  listingsController = controller
  loading.value = true; error.value = ''
  try {
    const response = await fetchListings(nextFilters, controller.signal)
    listings.value = response.data
    filters.value = { ...nextFilters, offset: response.pagination.offset }
    hasMore.value = response.pagination.offset + response.data.length < response.pagination.total
    if (selectedListingId.value && !listings.value.some((listing) => listing.id === selectedListingId.value)) selectedListingId.value = null
  } catch (caught) {
    if (!(caught instanceof DOMException && caught.name === 'AbortError')) error.value = caught instanceof Error ? caught.message : 'No se pudieron cargar los inmuebles'
  } finally { if (listingsController === controller) loading.value = false }
}

async function loadStats() { try { stats.value = await fetchStats() } catch { /* Catalog remains useful when stats are unavailable. */ } }
function applyFilters(next: ListingFilters) {
  mapSearchAvailable.value = false
  void loadListings(next)
}
function resetFilters() { const next = { ...filters.value, source: '', country_code: '', city: '', operation: '', property_type: '', min_price: '', max_price: '', min_area: '', max_area: '', min_longitude: '', max_longitude: '', min_latitude: '', max_latitude: '', offset: 0 }; applyFilters(next) }
async function selectListing(listing: Listing, source: 'marker-click' | 'listing-click') {
  selectedListingId.value = listing.id
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
  if (tab === 'map') syncMapDraft()
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
  applyFilters({ ...filters.value, ...mapDraft, offset: 0 })
}
function handleViewportChange(bounds: { minLongitude: number; maxLongitude: number; minLatitude: number; maxLatitude: number }) {
  mapViewport.value = bounds
  mapSearchAvailable.value = true
}
function searchCurrentZone() {
  if (!mapViewport.value) return
  mapSearchAvailable.value = false
  void loadListings({
    ...filters.value,
    min_longitude: String(mapViewport.value.minLongitude),
    max_longitude: String(mapViewport.value.maxLongitude),
    min_latitude: String(mapViewport.value.minLatitude),
    max_latitude: String(mapViewport.value.maxLatitude),
    offset: 0,
  })
}

onMounted(() => { syncMapDraft(); void loadListings(); void loadStats() })
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
            <div class="stats-card"><small>Total de propiedades</small><strong>{{ stats?.total ?? listings.length }}</strong></div>
            <div class="stats-card"><small>Última actualización</small><strong>{{ stats?.last_updated_at ? new Date(stats.last_updated_at).toLocaleDateString('es-CO') : '—' }}</strong></div>
          </div>
          <div class="stats-columns">
            <section><h2>Por fuente</h2><p v-for="(count, source) in stats?.by_source" :key="source"><span>{{ source }}</span><strong>{{ count }}</strong></p></section>
            <section><h2>Por operación</h2><p v-for="(count, operation) in stats?.by_operation" :key="operation"><span>{{ operation === 'SALE' ? 'Venta' : operation === 'RENT' ? 'Arriendo' : operation }}</span><strong>{{ count }}</strong></p></section>
          </div>
        </div>
        <div class="catalog-header"><div><span class="eyebrow">Inventario de {{ countryLabel }}</span><h1>Propiedades para <em>explorar</em></h1></div><div id="estadisticas" class="catalog-summary"><strong>{{ listings.length }}</strong><span>resultados<br />en esta vista</span></div></div>
        <FilterBar @apply="applyFilters" @reset="resetFilters" />
        <div class="active-filters"><span v-if="filters.country_code" class="filter-chip">{{ filters.country_code === 'UY' ? 'Uruguay' : 'Colombia' }} <button type="button" @click="applyFilters({ ...filters, country_code: '' })">×</button></span><span v-if="filters.city" class="filter-chip">{{ filters.city }} <button type="button" @click="applyFilters({ ...filters, city: '' })">×</button></span><span v-if="filters.operation" class="filter-chip">{{ filters.operation === 'SALE' ? 'Venta' : 'Arriendo' }} <button type="button" @click="applyFilters({ ...filters, operation: '' })">×</button></span><span v-if="filters.property_type" class="filter-chip">{{ filters.property_type }} <button type="button" @click="applyFilters({ ...filters, property_type: '' })">×</button></span></div>
        <div v-if="loading" class="state-panel"><div class="loader"></div><strong>Buscando propiedades</strong><span>Consultando el inventario real…</span></div>
        <div v-else-if="error" class="state-panel error-state"><span class="state-icon">!</span><strong>No pudimos cargar el catálogo</strong><span>{{ error }}</span><button class="secondary-button" type="button" @click="loadListings()">Reintentar</button></div>
        <div v-else-if="!listings.length" class="state-panel"><span class="state-icon">⌂</span><strong>No encontramos resultados</strong><span>Ajusta los filtros para ampliar tu búsqueda.</span></div>
        <template v-else>
          <div ref="listingScroll" class="listing-grid"><ListingCard v-for="listing in listings" :key="listing.id" :listing="listing" :selected="selected?.id === listing.id" :data-listing-id="listing.id" @select="selectListingFromCard" /></div>
          <div class="pagination"><button type="button" :disabled="filters.offset === 0" @click="movePage(-1)">← Anterior</button><span>Página {{ Math.floor(filters.offset / filters.limit) + 1 }}</span><button type="button" :disabled="!hasMore" @click="movePage(1)">Siguiente →</button></div>
        </template>
      </section>
      <aside id="mapa" class="map-column" :class="{ 'is-map-tab': activeTab === 'map' }">
        <div v-if="activeTab !== 'map'" class="map-heading"><div><span class="eyebrow">Vista geográfica</span><h2>Descubre por ubicación</h2></div><span class="map-count"><b>{{ locatedCount }}</b> con coordenadas</span></div>
        <div v-else class="map-explorer-toolbar">
          <div class="map-toolbar-title"><span class="eyebrow">Exploración geográfica</span><strong>{{ listings.length }} propiedades visibles</strong></div>
          <div class="map-toolbar-filters">
            <select v-model="mapDraft.country_code" aria-label="País"><option value="">Todos los países</option><option value="CO">Colombia</option><option value="UY">Uruguay</option></select>
            <select v-model="mapDraft.operation" aria-label="Operación"><option value="">Venta y arriendo</option><option value="SALE">Venta</option><option value="RENT">Arriendo</option></select>
            <select v-model="mapDraft.property_type" aria-label="Tipo de propiedad"><option value="">Todos los tipos</option><option value="APARTMENT">Apartamento</option><option value="HOUSE">Casa</option><option value="LAND">Lote</option><option value="COMMERCIAL">Comercial</option></select>
            <input v-model="mapDraft.city" type="text" placeholder="Ciudad o región" aria-label="Ciudad o región" />
            <input v-model="mapDraft.min_price" type="number" min="0" placeholder="Precio mínimo" aria-label="Precio mínimo" />
            <input v-model="mapDraft.max_price" type="number" min="0" placeholder="Precio máximo" aria-label="Precio máximo" />
            <button type="button" class="map-filter-button" @click="applyMapFilters">Aplicar</button>
          </div>
          <button v-if="mapSearchAvailable" type="button" class="map-zone-button" @click="searchCurrentZone">Buscar en esta zona</button>
        </div>
        <div class="map-stage">
          <ListingMap :listings="listings" :selected-id="selected?.id ?? null" @select="selectListingFromMarker" @viewport-change="handleViewportChange" />
          <aside v-if="activeTab === 'map'" class="map-list-panel" :class="{ open: mapListOpen }" aria-label="Propiedades en el mapa">
            <button type="button" class="map-list-toggle" @click="mapListOpen = !mapListOpen">{{ mapListOpen ? 'Ocultar lista' : 'Ver propiedades' }} <span aria-hidden="true">{{ mapListOpen ? '←' : '→' }}</span></button>
            <div v-show="mapListOpen" ref="mapListingScroll" class="map-list-scroll">
              <ListingCard v-for="listing in listings" :key="`map-${listing.id}`" :listing="listing" :selected="selected?.id === listing.id" :data-listing-id="listing.id" @select="selectListingFromCard" />
              <p v-if="!listings.length && !loading" class="map-list-empty">No hay propiedades para esta vista.</p>
            </div>
          </aside>
        </div>
      </aside>
    </main>
    <DetailDrawer :listing="selected" @close="closeDetail" />
  </div>
</template>
