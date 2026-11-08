<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { LngLatBounds, Map, Marker, NavigationControl, setWorkerUrl } from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import type { Listing } from '../types'
import {
  COLOMBIA_CENTER,
  getCoordinateDiagnostics,
  getValidCoordinates,
  getValidListings,
} from '../map/coordinates'
import { getInitialCameraAction, getSelectionCameraAction } from '../map/camera'

setWorkerUrl(workerUrl)

const props = defineProps<{ listings: Listing[]; selectedId: number | null }>()
type SelectionSource = 'marker-click'
const emit = defineEmits<{ select: [listing: Listing, source: SelectionSource] }>()
const mapElement = ref<HTMLElement | null>(null)
let map: Map | null = null
let mapReady = false
let initialFitDone = false
let resizeObserver: ResizeObserver | null = null
const markers = new globalThis.Map<number, Marker>()

const mappedListings = () => getValidListings(props.listings)
const coordinateDiagnostics = () => getCoordinateDiagnostics(props.listings)

function renderMarkers() {
  if (!map || !mapReady) return
  for (const marker of markers.values()) marker.remove()
  markers.clear()
  for (const listing of mappedListings()) {
    const node = document.createElement('button')
    node.className = `map-marker${listing.id === props.selectedId ? ' active' : ''}`
    node.type = 'button'
    node.title = listing.title
    node.innerHTML = '<span></span>'
    node.addEventListener('click', () => emit('select', listing, 'marker-click'))
    const coordinate = getValidCoordinates(listing)
    if (!coordinate) continue
    const marker = new Marker({ element: node }).setLngLat(coordinate).addTo(map)
    markers.set(listing.id, marker)
  }
}

function fitToListings() {
  if (!map) return
  const available = mappedListings()
  if (available.length === 0) {
    map.flyTo({ center: COLOMBIA_CENTER, zoom: 5, duration: 500 })
    return
  }
  const firstCoordinate = getValidCoordinates(available[0])
  if (!firstCoordinate) return
  if (available.length === 1) { map.flyTo({ center: firstCoordinate, zoom: 12, duration: 500 }); return }
  const bounds = new LngLatBounds()
  available.forEach((listing) => {
    const coordinate = getValidCoordinates(listing)
    if (coordinate) bounds.extend(coordinate)
  })
  map.fitBounds(bounds, { padding: 60, maxZoom: 13, duration: 500 })
}

function flyToSelectedListing(selectedId: number | null) {
  if (!map || !mapReady || selectedId == null) return
  const listing = props.listings.find((candidate) => candidate.id === selectedId)
  const coordinate = listing ? getValidCoordinates(listing) : null
  if (!coordinate) return
  map.flyTo({ center: coordinate, zoom: 12, duration: 500 })
}

function handleListingsChanged() {
  if (!mapReady) return
  renderMarkers()
  if (getInitialCameraAction(initialFitDone, props.listings.length) !== 'initial-fit') return
  initialFitDone = true
  fitToListings()
}

onMounted(() => {
  if (!mapElement.value) return
  map = new Map({ container: mapElement.value, style: 'https://tiles.openfreemap.org/styles/liberty', center: COLOMBIA_CENTER, zoom: 5 })
  resizeObserver = new ResizeObserver(() => map?.resize())
  resizeObserver.observe(mapElement.value)
  map.addControl(new NavigationControl({ showCompass: false }), 'top-right')
  map.on('load', () => {
    mapReady = true
    map?.resize()
    renderMarkers()
    if (getInitialCameraAction(initialFitDone, props.listings.length) === 'initial-fit') {
      initialFitDone = true
      fitToListings()
    }
  })
})

watch(() => props.listings, handleListingsChanged, { deep: true })
watch(() => props.selectedId, (selectedId, previousId) => {
  const listing = props.listings.find((candidate) => candidate.id === selectedId)
  const action = getSelectionCameraAction(previousId ?? null, selectedId, listing ? getValidCoordinates(listing) !== null : false)
  if (action === 'selection-fly') flyToSelectedListing(selectedId)
  renderMarkers()
})
onBeforeUnmount(() => { resizeObserver?.disconnect(); resizeObserver = null; map?.remove(); map = null; mapReady = false })
</script>

<template>
  <section class="map-shell">
    <div ref="mapElement" class="map-canvas"></div>
    <div class="map-legend"><span class="legend-dot"></span>{{ coordinateDiagnostics().withCoordinates }} ubicados en el mapa</div>
    <div v-if="coordinateDiagnostics().withoutCoordinates" class="map-note">{{ coordinateDiagnostics().withoutCoordinates }} sin coordenadas exactas</div>
    <div v-if="coordinateDiagnostics().invalidCoordinates" class="map-note">{{ coordinateDiagnostics().invalidCoordinates }} con coordenadas no válidas</div>
    <div v-if="!listings.length" class="map-empty"><span>⌖</span><strong>El mapa espera resultados</strong><small>Prueba con otros filtros</small></div>
  </section>
</template>
