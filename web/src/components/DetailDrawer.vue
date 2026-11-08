<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { fetchHistory, resolveImageUrl } from '../api/client'
import type { Listing, Snapshot } from '../types'

const props = defineProps<{ listing: Listing | null }>()
const emit = defineEmits<{ close: [] }>()
const history = ref<Snapshot[]>([])
const loading = ref(false)
const error = ref('')

const money = (value: number | null, currency: string | null) => value == null ? 'Sin precio' : new Intl.NumberFormat('es-CO', { style: 'currency', currency: currency || 'COP', maximumFractionDigits: 0 }).format(value)
const date = (value: string | null) => value ? new Intl.DateTimeFormat('es-CO', { dateStyle: 'medium' }).format(new Date(value)) : '—'
const current = computed(() => props.listing)

async function loadHistory() {
  if (!props.listing) return
  loading.value = true; error.value = ''
  try { history.value = await fetchHistory(props.listing.id) } catch (caught) { error.value = caught instanceof Error ? caught.message : 'No se pudo cargar el historial' } finally { loading.value = false }
}
watch(() => props.listing?.id, loadHistory)
onMounted(loadHistory)
</script>

<template>
  <aside v-if="current" class="detail-drawer">
    <button class="close-button" type="button" aria-label="Cerrar detalle" @click="emit('close')">×</button>
    <div v-if="current.images?.length" class="detail-gallery"><img :src="resolveImageUrl(current.images[0])" :alt="current.title" /></div>
    <div class="detail-body">
      <span class="eyebrow">{{ current.source }} · {{ current.external_id }}</span>
      <h2>{{ current.title }}</h2>
      <p class="detail-price">{{ money(current.price, current.currency) }}</p>
      <p class="detail-location">{{ current.city }}<span v-if="current.neighborhood"> · {{ current.neighborhood }}</span></p>
      <div class="detail-spec-grid">
        <div><small>Área</small><strong>{{ current.area_m2 != null ? `${current.area_m2} m²` : '—' }}</strong></div>
        <div><small>Habitaciones</small><strong>{{ current.bedrooms ?? '—' }}</strong></div>
        <div><small>Baños</small><strong>{{ current.bathrooms ?? '—' }}</strong></div>
        <div><small>Parqueaderos</small><strong>{{ current.parking_spots ?? '—' }}</strong></div>
      </div>
      <p v-if="current.description" class="detail-description">{{ current.description }}</p>
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
