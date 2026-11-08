<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchHistory, refreshListingDetails, resolveImageUrl } from '../api/client'
import type { Listing, Snapshot } from '../types'
import { getLocationPrecision } from '../map/coordinates'

const props = defineProps<{ listing: Listing | null }>()
const emit = defineEmits<{ close: [] }>()
const history = ref<Snapshot[]>([])
const loading = ref(false)
const error = ref('')
const detailsLoading = ref(false)
const detailsError = ref('')
const imageFailed = ref(false)
const selectedImage = ref(0)
const details = ref<Listing | null>(null)
let detailsController: AbortController | null = null
let detailsRequestSequence = 0
const detailsCache = new Map<number, Listing>()

const money = (value: number | null, currency: string | null) => value == null ? 'Sin precio' : new Intl.NumberFormat('es-CO', { style: 'currency', currency: currency || 'COP', maximumFractionDigits: 0 }).format(value)
const date = (value: string | null) => value ? new Intl.DateTimeFormat('es-CO', { dateStyle: 'medium' }).format(new Date(value)) : '—'
const current = computed(() => details.value ?? props.listing)
const locationStatus = computed(() => {
  if (!current.value) return 'Ubicación no disponible'
  const precision = getLocationPrecision(current.value)
  if (precision === 'EXACT') return 'Ubicación exacta'
  if (precision === 'CITY') return 'Solo ciudad disponible'
  if (precision === 'UNKNOWN') return 'Ubicación no disponible'
  return 'Ubicación aproximada'
})

async function loadHistory() {
  if (!props.listing) return
  loading.value = true; error.value = ''
  try { history.value = await fetchHistory(props.listing.id) } catch (caught) { error.value = caught instanceof Error ? caught.message : 'No se pudo cargar el historial' } finally { loading.value = false }
}
async function loadDetails() {
  const listing = props.listing
  if (!listing) return
  const requestSequence = ++detailsRequestSequence
  detailsController?.abort()
  detailsController = new AbortController()
  detailsError.value = ''

  const cached = detailsCache.get(listing.id)
  if (cached && cached.details_status === 'READY' && cached.details_expires_at && new Date(cached.details_expires_at) > new Date()) {
    details.value = cached
    return
  }
  if (listing.details_status === 'READY' && listing.details_expires_at && new Date(listing.details_expires_at) > new Date()) return

  detailsLoading.value = true
  try {
    const refreshed = await refreshListingDetails(listing.id, detailsController.signal)
    if (requestSequence !== detailsRequestSequence || props.listing?.id !== listing.id) return
    details.value = refreshed
    detailsCache.set(listing.id, refreshed)
  } catch (caught) {
    if (requestSequence !== detailsRequestSequence || (caught instanceof DOMException && caught.name === 'AbortError')) return
    detailsError.value = caught instanceof Error ? caught.message : 'No se pudieron actualizar los detalles'
  } finally {
    if (requestSequence === detailsRequestSequence) detailsLoading.value = false
  }
}

function resetForListing() {
  detailsController?.abort()
  detailsController = null
  detailsRequestSequence++
  details.value = props.listing
  detailsError.value = ''
  imageFailed.value = false
  selectedImage.value = 0
  void loadDetails()
  void loadHistory()
}

watch(() => props.listing?.id, resetForListing)
watch(() => current.value?.images?.[0], () => { imageFailed.value = false })
onMounted(resetForListing)
onBeforeUnmount(() => detailsController?.abort())
</script>

<template>
  <aside v-if="current" class="detail-drawer">
    <button class="close-button" type="button" aria-label="Cerrar detalle" @click="emit('close')">×</button>
    <div v-if="current.images?.length && !imageFailed" class="detail-gallery">
      <img :src="resolveImageUrl(current.images[selectedImage])" :alt="current.title" @error="imageFailed = true" />
      <div v-if="current.images.length > 1" class="detail-thumbnails" role="tablist" aria-label="Fotos del inmueble">
        <button v-for="(image, index) in current.images" :key="image" type="button" :class="{ active: selectedImage === index }" role="tab" :aria-selected="selectedImage === index" :aria-label="`Ver foto ${index + 1}`" @click="selectedImage = index"><img :src="resolveImageUrl(image)" alt="" loading="lazy" /></button>
      </div>
    </div>
    <div class="detail-body">
      <span class="eyebrow">{{ current.source }} · {{ current.external_id }}</span>
      <h2>{{ current.title }}</h2>
      <p class="detail-price">{{ money(current.price, current.currency) }}</p>
      <p class="detail-location">{{ current.city }}<span v-if="current.neighborhood"> · {{ current.neighborhood }}</span></p>
      <p class="detail-location-precision">{{ locationStatus }}</p>
      <div class="detail-spec-grid">
        <div><small>Área</small><strong>{{ current.area_m2 != null ? `${current.area_m2} m²` : '—' }}</strong></div>
        <div><small>Habitaciones</small><strong>{{ current.bedrooms ?? '—' }}</strong></div>
        <div><small>Baños</small><strong>{{ current.bathrooms ?? '—' }}</strong></div>
        <div><small>Parqueaderos</small><strong>{{ current.parking_spots ?? '—' }}</strong></div>
      </div>
      <p v-if="current.description" class="detail-description">{{ current.description }}</p>
      <div v-if="detailsLoading" class="inline-state">Actualizando detalles…</div>
      <div v-else-if="detailsError" class="inline-state error-text">{{ detailsError }} <button type="button" class="text-button" @click="loadDetails">Reintentar</button></div>
      <div class="observation-row"><span>Primer registro <strong>{{ date(current.first_seen_at) }}</strong></span><span>Último registro <strong>{{ date(current.last_seen_at) }}</strong></span></div>
      <section class="history-section">
        <div class="section-title"><span>Historial de precio</span><span class="history-count">{{ history.length }}</span></div>
        <div v-if="loading" class="inline-state">Cargando historial…</div>
        <div v-else-if="error" class="inline-state error-text">{{ error }}</div>
        <div v-else-if="!history.length" class="inline-state">Sin cambios registrados.</div>
        <div v-else class="history-list"><div v-for="item in history" :key="item.id" class="history-item"><span>{{ date(item.collected_at) }}</span><strong>{{ money(item.price, item.currency) }}</strong></div></div>
      </section>
      <a class="original-link" :href="current.url" target="_blank" rel="noreferrer">Ver publicación original <span>↗</span></a>
    </div>
  </aside>
</template>
