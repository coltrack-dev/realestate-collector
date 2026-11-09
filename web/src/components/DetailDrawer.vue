<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchHistory, refreshListingDetails, resolveImageUrl } from '../api/client'
import type { Listing, Snapshot } from '../types'
import { getLocationPrecision } from '../map/coordinates'

const props = defineProps<{ listing: Listing | null; open: boolean }>()
const emit = defineEmits<{ close: []; toggle: [] }>()
const history = ref<Snapshot[]>([])
const loading = ref(false)
const error = ref('')
const detailsLoading = ref(false)
const detailsError = ref('')
const imageFailed = ref(false)
const selectedImage = ref(0)
const isImageModalOpen = ref(false)
const details = ref<Listing | null>(null)
const modalRoot = ref<HTMLElement | null>(null)
const toggleButton = ref<HTMLButtonElement | null>(null)
let detailsController: AbortController | null = null
let detailsRequestSequence = 0
let previousFocusedElement: HTMLElement | null = null
const detailsCache = new Map<number, Listing>()

const money = (value: number | null, currency: string | null) => value == null ? 'Sin precio' : new Intl.NumberFormat('es-CO', { style: 'currency', currency: currency || 'COP', maximumFractionDigits: 0 }).format(value)
const date = (value: string | null) => value ? new Intl.DateTimeFormat('es-CO', { dateStyle: 'medium' }).format(new Date(value)) : '—'
const current = computed(() => details.value ?? props.listing)
const photos = computed(() => current.value?.images ?? [])
const currentPhoto = computed(() => photos.value[selectedImage.value] ?? null)
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
  loading.value = true
  error.value = ''
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
  if (cached && cached.details_status === 'READY' && cached.details_expires_at && new Date(cached.details_expires_at) > new Date()) { details.value = cached; return }
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
  } finally { if (requestSequence === detailsRequestSequence) detailsLoading.value = false }
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

function selectImage(index: number) {
  if (index < 0 || index >= photos.value.length) return
  selectedImage.value = index
  imageFailed.value = false
}

function moveImage(direction: number) {
  if (photos.value.length < 2) return
  selectImage((selectedImage.value + direction + photos.value.length) % photos.value.length)
}

function openFromMainPhoto() {
  if (props.open && !isImageModalOpen.value && currentPhoto.value) void openExpanded()
}

function handleMainPhotoKeydown(event: KeyboardEvent) {
  if (isImageModalOpen.value || !currentPhoto.value) return
  if (event.target !== event.currentTarget) return
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    openFromMainPhoto()
  }
}

function focusableElements(): HTMLElement[] {
  if (!modalRoot.value) return []
  return Array.from(modalRoot.value.querySelectorAll<HTMLElement>('button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'))
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    if (isImageModalOpen.value) closeImageModal()
    else if (props.open) emit('close')
    return
  }
  if (!isImageModalOpen.value) return
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
    event.preventDefault()
    moveImage(event.key === 'ArrowLeft' ? -1 : 1)
    return
  }
  if (event.key !== 'Tab') return
  const focusable = focusableElements()
  if (!focusable.length) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (!modalRoot.value?.contains(document.activeElement)) {
    event.preventDefault()
    ;(event.shiftKey ? last : first).focus()
    return
  }
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}

async function openExpanded() {
  previousFocusedElement = document.activeElement instanceof HTMLElement ? document.activeElement : null
  isImageModalOpen.value = true
  document.body.classList.add('detail-modal-open')
  await nextTick()
  toggleButton.value?.focus()
}

function closeImageModal() {
  isImageModalOpen.value = false
  document.body.classList.remove('detail-modal-open')
  void nextTick(() => {
    if (previousFocusedElement?.isConnected) previousFocusedElement.focus()
    else toggleButton.value?.focus()
  })
}

function togglePanel() {
  if (isImageModalOpen.value) {
    closeImageModal()
    return
  }
  emit('toggle')
}

watch(() => props.listing?.id, resetForListing)
watch(() => current.value?.images?.length, (length) => {
  imageFailed.value = false
  if (!length) selectedImage.value = 0
  else if (selectedImage.value >= length) selectedImage.value = length - 1
})
onMounted(() => { resetForListing(); document.addEventListener('keydown', handleKeydown) })
onBeforeUnmount(() => { detailsController?.abort(); document.removeEventListener('keydown', handleKeydown); document.body.classList.remove('detail-modal-open') })
</script>

<template>
  <div v-if="current" class="detail-layer">
    <div v-if="open && isImageModalOpen" class="detail-modal-backdrop" aria-hidden="true" @click="closeImageModal"></div>
    <aside ref="modalRoot" class="detail-drawer" :class="{ 'is-expanded': isImageModalOpen, 'is-closed': !open }" :role="open && isImageModalOpen ? 'dialog' : 'complementary'" :aria-modal="open && isImageModalOpen ? 'true' : undefined" :aria-label="open && isImageModalOpen ? 'Detalle de la propiedad' : undefined">
      <header class="detail-header">
        <span v-if="open" class="eyebrow">{{ current.source }} · {{ current.external_id }}</span>
        <button ref="toggleButton" class="detail-toggle-button" type="button" :aria-expanded="open" :aria-label="isImageModalOpen ? 'Cerrar fotografía' : open ? 'Cerrar detalles' : 'Abrir detalles'" :title="isImageModalOpen ? 'Cerrar fotografía' : open ? 'Cerrar detalles' : 'Abrir detalles'" @click.stop="togglePanel"><span aria-hidden="true">{{ open ? '×' : '⤢' }}</span></button>
      </header>

      <template v-if="open">
      <section class="detail-gallery" aria-label="Galería de fotografías">
        <div v-if="currentPhoto && !imageFailed" class="detail-gallery-main" :class="{ 'is-zoomable': !isImageModalOpen }" :role="isImageModalOpen ? undefined : 'button'" :tabindex="isImageModalOpen ? -1 : 0" :aria-label="isImageModalOpen ? undefined : 'Ampliar fotografía'" @click="openFromMainPhoto" @keydown="handleMainPhotoKeydown">
          <button v-if="photos.length > 1" class="gallery-arrow gallery-arrow-prev" type="button" aria-label="Fotografía anterior" @click.stop="moveImage(-1)">‹</button>
          <img :src="resolveImageUrl(currentPhoto)" :alt="current.title" @error="imageFailed = true" />
          <button v-if="photos.length > 1" class="gallery-arrow gallery-arrow-next" type="button" aria-label="Fotografía siguiente" @click.stop="moveImage(1)">›</button>
        </div>
        <div v-else class="detail-gallery-placeholder"><span aria-hidden="true">⌂</span><small>Sin fotografías disponibles</small></div>
        <div v-if="photos.length" class="detail-thumbnails" role="tablist" aria-label="Fotos del inmueble">
          <button v-for="(image, index) in photos" :key="`${image}-${index}`" type="button" :class="{ active: selectedImage === index }" role="tab" :aria-selected="selectedImage === index" :aria-label="`Ver foto ${index + 1}`" @click="selectImage(index)"><img :src="resolveImageUrl(image)" alt="" loading="lazy" decoding="async" /></button>
        </div>
      </section>

      <div class="detail-body">
        <h2>{{ current.title }}</h2>
        <p class="detail-price">{{ money(current.price, current.currency) }}</p>
        <p class="detail-location">{{ current.city }}<span v-if="current.neighborhood"> · {{ current.neighborhood }}</span></p>
        <p class="detail-location-precision">{{ locationStatus }}</p>
        <div class="detail-spec-grid"><div><small>Área</small><strong>{{ current.area_m2 != null ? `${current.area_m2} m²` : '—' }}</strong></div><div><small>Habitaciones</small><strong>{{ current.bedrooms ?? '—' }}</strong></div><div><small>Baños</small><strong>{{ current.bathrooms ?? '—' }}</strong></div><div><small>Parqueaderos</small><strong>{{ current.parking_spots ?? '—' }}</strong></div></div>
        <p v-if="current.description" class="detail-description">{{ current.description }}</p>
        <div v-if="detailsLoading" class="inline-state">Actualizando detalles…</div>
        <div v-else-if="detailsError" class="inline-state error-text">{{ detailsError }} <button type="button" class="text-button" @click="loadDetails">Reintentar</button></div>
        <div class="observation-row"><span>Primer registro <strong>{{ date(current.first_seen_at) }}</strong></span><span>Último registro <strong>{{ date(current.last_seen_at) }}</strong></span></div>
        <section class="history-section"><div class="section-title"><span>Historial de precio</span><span class="history-count">{{ history.length }}</span></div><div v-if="loading" class="inline-state">Cargando historial…</div><div v-else-if="error" class="inline-state error-text">{{ error }}</div><div v-else-if="!history.length" class="inline-state">Sin cambios registrados.</div><div v-else class="history-list"><div v-for="item in history" :key="item.id" class="history-item"><span>{{ date(item.collected_at) }}</span><strong>{{ money(item.price, item.currency) }}</strong></div></div></section>
        <a class="original-link" :href="current.url" target="_blank" rel="noreferrer">Ver publicación original <span>↗</span></a>
      </div>
      </template>
    </aside>
  </div>
</template>
