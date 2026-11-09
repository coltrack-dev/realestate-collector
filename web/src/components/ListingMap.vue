<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { LngLatBounds, Map, Marker, NavigationControl, Popup, setWorkerUrl } from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import type { Listing } from '../types'
import { resolveImageUrl } from '../api/client'
import {
  COLOMBIA_CENTER,
  URUGUAY_CENTER,
  getCoordinateDiagnostics,
  getLocationPrecision,
  getNavigationTarget,
  getValidCoordinates,
  getValidListings,
} from '../map/coordinates'
import { getSelectionCameraAction, zoomForPrecision } from '../map/camera'

setWorkerUrl(workerUrl)

const props = defineProps<{ listings: Listing[]; selectedId: number | null }>()
type SelectionSource = 'marker-click'
type ViewportBounds = { minLongitude: number; maxLongitude: number; minLatitude: number; maxLatitude: number }
const emit = defineEmits<{ select: [listing: Listing, source: SelectionSource]; 'viewport-change': [bounds: ViewportBounds] }>()
const mapElement = ref<HTMLElement | null>(null)
let map: Map | null = null
let mapReady = false
let initialFitCountry: string | null = null
let resizeObserver: ResizeObserver | null = null
let popup: Popup | null = null
let popupHideTimer: ReturnType<typeof setTimeout> | null = null
let popupHoverBound = false
let userCameraChange = false
let programmaticCameraChange = false
const markers = new globalThis.Map<number, Marker>()

const mappedListings = () => getValidListings(props.listings)
const coordinateDiagnostics = () => getCoordinateDiagnostics(props.listings)

function formatType(value: string): string {
  return value.replaceAll('_', ' ').toLowerCase().replace(/^./, (letter) => letter.toUpperCase())
}

function formatPrice(listing: Listing): string | null {
  if (listing.price == null) return null
  const value = new Intl.NumberFormat('es-CO', { maximumFractionDigits: 0 }).format(listing.price)
  return `${value} ${listing.currency || 'COP'}`
}

function appendPopupField(container: HTMLElement, label: string, value: string | null): void {
  if (!value) return
  const field = document.createElement('div')
  field.className = 'listing-map-popup__field'
  const labelElement = document.createElement('span')
  labelElement.className = 'listing-map-popup__label'
  labelElement.textContent = label
  const valueElement = document.createElement('strong')
  valueElement.textContent = value
  field.append(labelElement, valueElement)
  container.append(field)
}

function createPopupContent(listing: Listing): HTMLElement {
  const content = document.createElement('article')
  content.className = 'listing-map-popup'

  const imageURL = listing.images?.[0]
  if (imageURL) {
    const image = document.createElement('img')
    image.className = 'listing-map-popup__image'
    image.src = resolveImageUrl(imageURL)
    image.alt = ''
    image.loading = 'lazy'
    image.addEventListener('error', () => image.remove(), { once: true })
    content.append(image)
  }

  const title = document.createElement('h3')
  title.className = 'listing-map-popup__title'
  title.textContent = listing.title || 'Inmueble'
  content.append(title)

  const fields = document.createElement('div')
  fields.className = 'listing-map-popup__fields'
  appendPopupField(fields, 'Tipo', listing.property_type ? formatType(listing.property_type) : null)
  appendPopupField(fields, 'Precio', formatPrice(listing))
  appendPopupField(fields, 'Área', listing.area_m2 != null ? `${listing.area_m2} m²` : null)
  appendPopupField(fields, 'Ubicación', [listing.city, listing.neighborhood].filter(Boolean).join(' · ') || null)
  appendPopupField(fields, 'Fuente', listing.source === 'dccolombia' ? 'D.C Colombia' : formatType(listing.source))
  content.append(fields)
  return content
}

function cancelPopupHide(): void {
  if (popupHideTimer) clearTimeout(popupHideTimer)
  popupHideTimer = null
}

function schedulePopupHide(): void {
  cancelPopupHide()
  popupHideTimer = setTimeout(() => popup?.remove(), 120)
}

function bindPopupHover(): void {
  if (!popup || popupHoverBound) return
  const popupElement = popup.getElement()
  if (!popupElement) return
  popupElement.addEventListener('pointerenter', cancelPopupHide)
  popupElement.addEventListener('pointerleave', schedulePopupHide)
  popupHoverBound = true
}

function showPopup(listing: Listing): void {
  if (!map || !mapReady) return
  const coordinate = getValidCoordinates(listing)
  if (!coordinate) return
  cancelPopupHide()
  popup ??= new Popup({ closeButton: false, closeOnClick: false, maxWidth: '280px', offset: 16 })
  popup.setDOMContent(createPopupContent(listing)).setLngLat(coordinate).addTo(map)
  bindPopupHover()
}

function renderMarkers() {
  if (!map || !mapReady) return
  for (const marker of markers.values()) marker.remove()
  markers.clear()
  popup?.remove()
  for (const listing of mappedListings()) {
    const node = document.createElement('button')
    node.className = `map-marker${listing.id === props.selectedId ? ' active' : ''}`
    node.type = 'button'
    node.title = listing.title
    const markerDot = document.createElement('span')
    node.append(markerDot)
    node.addEventListener('pointerenter', () => showPopup(listing))
    node.addEventListener('pointerleave', schedulePopupHide)
    node.addEventListener('click', () => {
      showPopup(listing)
      emit('select', listing, 'marker-click')
    })
    const coordinate = getValidCoordinates(listing)
    if (!coordinate) continue
    const marker = new Marker({ element: node }).setLngLat(coordinate).addTo(map)
    markers.set(listing.id, marker)
  }
}

function fitToListings() {
  if (!map) return
  programmaticCameraChange = true
  const available = mappedListings()
  if (available.length === 0) {
    const center = props.listings[0]?.country_code === 'UY' ? URUGUAY_CENTER : COLOMBIA_CENTER
    map.flyTo({ center, zoom: 5, duration: 500 })
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
  const target = listing ? getNavigationTarget(listing) : null
  if (!target) return
  programmaticCameraChange = true
  map.flyTo({ center: target.coordinate, zoom: zoomForPrecision(target.precision), duration: 500 })
}

function updateMarkerStates(): void {
  for (const [listingId, marker] of markers) {
    marker.getElement().classList.toggle('active', listingId === props.selectedId)
  }
}

function markUserCameraChange(): void {
  userCameraChange = true
}

function emitUserViewport(): void {
  if (programmaticCameraChange) {
    programmaticCameraChange = false
    userCameraChange = false
    return
  }
  if (!map || !userCameraChange) return
  userCameraChange = false
  const bounds = map.getBounds()
  emit('viewport-change', {
    minLongitude: bounds.getWest(),
    maxLongitude: bounds.getEast(),
    minLatitude: bounds.getSouth(),
    maxLatitude: bounds.getNorth(),
  })
}

function handleListingsChanged() {
  if (!mapReady) return
  renderMarkers()
  const country = props.listings[0]?.country_code ?? null
  if (!country || initialFitCountry === country) return
  initialFitCountry = country
  fitToListings()
}

onMounted(() => {
  if (!mapElement.value) return
  map = new Map({ container: mapElement.value, style: 'https://tiles.openfreemap.org/styles/liberty', center: COLOMBIA_CENTER, zoom: 5 })
  resizeObserver = new ResizeObserver(() => map?.resize())
  resizeObserver.observe(mapElement.value)
  map.addControl(new NavigationControl({ showCompass: false }), 'top-right')
  map.on('dragstart', markUserCameraChange)
  map.on('zoomstart', markUserCameraChange)
  map.on('rotatestart', markUserCameraChange)
  map.on('moveend', emitUserViewport)
  map.on('load', () => {
    mapReady = true
    map?.resize()
    renderMarkers()
    const country = props.listings[0]?.country_code ?? null
    if (country && initialFitCountry !== country) {
      initialFitCountry = country
      fitToListings()
    }
  })
})

watch(() => props.listings, handleListingsChanged, { deep: true })
watch(() => props.selectedId, (selectedId, previousId) => {
  const listing = props.listings.find((candidate) => candidate.id === selectedId)
  const action = getSelectionCameraAction(previousId ?? null, selectedId, listing ? getLocationPrecision(listing) : 'UNKNOWN')
  if (action === 'selection-fly') flyToSelectedListing(selectedId)
  updateMarkerStates()
})
onBeforeUnmount(() => {
  cancelPopupHide()
  popup?.remove()
  popup = null
  popupHoverBound = false
  resizeObserver?.disconnect()
  resizeObserver = null
  userCameraChange = false
  programmaticCameraChange = false
  map?.remove()
  map = null
  mapReady = false
})
</script>

<template>
  <section class="map-shell">
    <div ref="mapElement" class="map-canvas"></div>
    <div class="map-legend"><span class="legend-dot"></span>{{ coordinateDiagnostics().withCoordinates }} ubicados en el mapa</div>
    <div v-if="coordinateDiagnostics().withoutCoordinates" class="map-note">{{ coordinateDiagnostics().withoutCoordinates }} sin coordenadas exactas</div>
    <div v-if="coordinateDiagnostics().invalidCoordinates" class="map-note">{{ coordinateDiagnostics().invalidCoordinates }} con coordenadas no válidas</div>
  </section>
</template>
