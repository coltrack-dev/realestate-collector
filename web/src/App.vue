<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import FilterBar from './components/FilterBar.vue'
import ListingCard from './components/ListingCard.vue'
import ListingMap from './components/ListingMap.vue'
import DetailDrawer from './components/DetailDrawer.vue'
import { fetchListings, fetchStats } from './api/client'
import type { Listing, ListingFilters, Stats } from './types'
import { getValidCoordinates } from './map/coordinates'

const listings = ref<Listing[]>([])
const stats = ref<Stats | null>(null)
const selectedListingId = ref<number | null>(null)
const filters = ref<ListingFilters>({ source: '', city: '', operation: '', property_type: '', min_price: '', max_price: '', min_area: '', max_area: '', limit: 50, offset: 0 })
const loading = ref(true)
const error = ref('')
const hasMore = ref(false)
const listingScroll = ref<HTMLElement | null>(null)

const selected = computed(() => listings.value.find((listing) => listing.id === selectedListingId.value) ?? null)
const locatedCount = computed(() => listings.value.filter((listing) => getValidCoordinates(listing) !== null).length)
const totalLabel = computed(() => stats.value?.total ?? 0)

async function loadListings(nextFilters = filters.value) {
  loading.value = true; error.value = ''
  try {
    const response = await fetchListings(nextFilters)
    listings.value = response.data
    filters.value = { ...nextFilters, offset: response.pagination.offset }
    hasMore.value = response.pagination.offset + response.data.length < response.pagination.total
    if (selectedListingId.value && !listings.value.some((listing) => listing.id === selectedListingId.value)) selectedListingId.value = null
  } catch (caught) { error.value = caught instanceof Error ? caught.message : 'No se pudieron cargar los inmuebles' } finally { loading.value = false }
}

async function loadStats() { try { stats.value = await fetchStats() } catch { /* Catalog remains useful when stats are unavailable. */ } }
function applyFilters(next: ListingFilters) { selectedListingId.value = null; void loadListings(next) }
function resetFilters() { const next = { ...filters.value, source: '', city: '', operation: '', property_type: '', min_price: '', max_price: '', min_area: '', max_area: '', offset: 0 }; applyFilters(next) }
async function selectListing(listing: Listing, source: 'marker-click' | 'listing-click') {
  selectedListingId.value = listing.id
  if (source !== 'marker-click') return
  await nextTick()
  const container = listingScroll.value
  const card = container?.querySelector<HTMLElement>(`[data-listing-id="${listing.id}"]`)
  if (!container || !card) return
  const top = card.offsetTop - Math.max(0, (container.clientHeight - card.offsetHeight) / 2)
  container.scrollTo({ top: Math.max(0, top), behavior: 'smooth' })
}
function selectListingFromCard(listing: Listing) { void selectListing(listing, 'listing-click') }
function selectListingFromMarker(listing: Listing) { void selectListing(listing, 'marker-click') }
function closeDetail() { selectedListingId.value = null }
function movePage(direction: number) { void loadListings({ ...filters.value, offset: Math.max(0, filters.value.offset + direction * filters.value.limit) }) }

onMounted(() => { void loadListings(); void loadStats() })
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <a class="brand" href="/" aria-label="Atlas Inmobiliario">
        <span class="brand-mark"><span></span><span></span><span></span></span>
        <span><strong>atlas</strong><small>inmobiliario</small></span>
      </a>
      <div class="topbar-meta"><span class="live-dot"></span><span>Inventario actualizado</span><span class="meta-divider"></span><span>Colombia</span></div>
      <div class="topbar-actions"><span class="data-pill">{{ totalLabel }} activos</span><button class="icon-button" type="button" aria-label="Menú">☰</button></div>
    </header>

    <main class="workspace">
      <aside class="sidebar"><FilterBar @apply="applyFilters" @reset="resetFilters" /><div class="sidebar-note"><span>✦</span><p>Precios y disponibilidad corresponden al último registro de cada fuente.</p></div></aside>
      <section class="catalog-column">
        <div class="catalog-header"><div><span class="eyebrow">Explorar inventario</span><h1>Propiedades que <em>encajan</em> contigo</h1></div><div class="catalog-summary"><strong>{{ listings.length }}</strong><span>resultados<br />en esta vista</span></div></div>
        <div class="active-filters"><span v-if="filters.city" class="filter-chip">{{ filters.city }} <button type="button" @click="applyFilters({ ...filters, city: '' })">×</button></span><span v-if="filters.operation" class="filter-chip">{{ filters.operation === 'SALE' ? 'Venta' : 'Arriendo' }} <button type="button" @click="applyFilters({ ...filters, operation: '' })">×</button></span><span v-if="filters.property_type" class="filter-chip">{{ filters.property_type }} <button type="button" @click="applyFilters({ ...filters, property_type: '' })">×</button></span></div>
        <div v-if="loading" class="state-panel"><div class="loader"></div><strong>Buscando propiedades</strong><span>Consultando el inventario real…</span></div>
        <div v-else-if="error" class="state-panel error-state"><span class="state-icon">!</span><strong>No pudimos cargar el catálogo</strong><span>{{ error }}</span><button class="secondary-button" type="button" @click="loadListings()">Reintentar</button></div>
        <div v-else-if="!listings.length" class="state-panel"><span class="state-icon">⌂</span><strong>No encontramos resultados</strong><span>Ajusta los filtros para ampliar tu búsqueda.</span></div>
        <template v-else>
          <div ref="listingScroll" class="listing-grid"><ListingCard v-for="listing in listings" :key="listing.id" :listing="listing" :selected="selected?.id === listing.id" :data-listing-id="listing.id" @select="selectListingFromCard" /></div>
          <div class="pagination"><button type="button" :disabled="filters.offset === 0" @click="movePage(-1)">← Anterior</button><span>Página {{ Math.floor(filters.offset / filters.limit) + 1 }}</span><button type="button" :disabled="!hasMore" @click="movePage(1)">Siguiente →</button></div>
        </template>
      </section>
      <aside class="map-column"><div class="map-heading"><div><span class="eyebrow">Vista geográfica</span><h2>Descubre por ubicación</h2></div><span class="map-count"><b>{{ locatedCount }}</b> con coordenadas</span></div><ListingMap :listings="listings" :selected-id="selected?.id ?? null" @select="selectListingFromMarker" /></aside>
    </main>
    <DetailDrawer :listing="selected" @close="closeDetail" />
  </div>
</template>
