<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { LngLatBounds, Map, Marker, NavigationControl, setWorkerUrl } from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import type { Listing } from '../types'

setWorkerUrl(workerUrl)

const props = defineProps<{ listings: Listing[]; selectedId: number | null }>()
const emit = defineEmits<{ select: [listing: Listing] }>()
const mapElement = ref<HTMLElement | null>(null)
let map: Map | null = null
const markers = new globalThis.Map<number, Marker>()

const mappedListings = () => props.listings.filter((listing) => listing.latitude != null && listing.longitude != null)

function renderMarkers() {
  if (!map) return
  for (const marker of markers.values()) marker.remove()
  markers.clear()
  for (const listing of mappedListings()) {
    const node = document.createElement('button')
    node.className = `map-marker${listing.id === props.selectedId ? ' active' : ''}`
    node.type = 'button'
    node.title = listing.title
    node.innerHTML = '<span></span>'
    node.addEventListener('click', () => emit('select', listing))
    const marker = new Marker({ element: node }).setLngLat([listing.longitude!, listing.latitude!]).addTo(map)
    markers.set(listing.id, marker)
  }
}

function fitToListings() {
  if (!map) return
  const available = mappedListings()
  if (available.length === 0) return
  if (available.length === 1) { map.flyTo({ center: [available[0].longitude!, available[0].latitude!], zoom: 12, duration: 500 }); return }
  const bounds = new LngLatBounds()
  available.forEach((listing) => bounds.extend([listing.longitude!, listing.latitude!]))
  map.fitBounds(bounds, { padding: 60, maxZoom: 13, duration: 500 })
}

onMounted(() => {
  if (!mapElement.value) return
  map = new Map({ container: mapElement.value, style: 'https://demotiles.maplibre.org/style.json', center: [-74.08, 4.65], zoom: 5 })
  map.addControl(new NavigationControl({ showCompass: false }), 'top-right')
  map.on('load', () => { renderMarkers(); fitToListings() })
})

watch(() => [props.listings, props.selectedId], () => { renderMarkers(); fitToListings() }, { deep: true })
onBeforeUnmount(() => { map?.remove(); map = null })
</script>

<template>
  <section class="map-shell">
    <div ref="mapElement" class="map-canvas"></div>
    <div class="map-legend"><span class="legend-dot"></span>{{ mappedListings().length }} ubicados en el mapa</div>
    <div v-if="mappedListings().length < listings.length" class="map-note">{{ listings.length - mappedListings().length }} sin coordenadas exactas</div>
    <div v-if="!listings.length" class="map-empty"><span>⌖</span><strong>El mapa espera resultados</strong><small>Prueba con otros filtros</small></div>
  </section>
</template>
