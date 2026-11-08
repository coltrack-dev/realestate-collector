<script setup lang="ts">
import { ref, watch } from 'vue'
import type { Listing } from '../types'
import { resolveImageUrl } from '../api/client'

const props = defineProps<{ listing: Listing; selected?: boolean }>()
defineEmits<{ select: [listing: Listing] }>()
const imageFailed = ref(false)
watch(() => props.listing.images?.[0], () => { imageFailed.value = false })

const money = (value: number | null, currency: string | null) => value == null ? 'Precio no publicado' : new Intl.NumberFormat('es-CO', { style: 'currency', currency: currency || 'COP', maximumFractionDigits: 0 }).format(value)
const label = (value: string) => value.replaceAll('_', ' ').toLowerCase().replace(/^./, (letter) => letter.toUpperCase())
</script>

<template>
  <article class="listing-card" :class="{ selected }" @click="$emit('select', listing)">
    <div class="card-image-wrap">
      <img v-if="listing.images?.[0] && !imageFailed" :src="resolveImageUrl(listing.images[0])" :alt="listing.title" class="card-image" loading="lazy" @error="imageFailed = true" />
      <div v-else class="image-placeholder"><span>⌂</span><small>Sin fotografía</small></div>
      <span class="source-badge">{{ label(listing.source) }}</span>
      <span class="operation-badge">{{ listing.operation === 'RENT' ? 'Arriendo' : 'Venta' }}</span>
    </div>
    <div class="card-content">
      <div class="card-price">{{ money(listing.price, listing.currency) }}</div>
      <h3>{{ listing.title || 'Inmueble sin título' }}</h3>
      <p class="card-location">{{ listing.city || 'Colombia' }}<span v-if="listing.neighborhood"> · {{ listing.neighborhood }}</span></p>
      <div class="property-specs">
        <span v-if="listing.area_m2 != null"><strong>{{ listing.area_m2 }}</strong> m²</span>
        <span v-if="listing.bedrooms != null"><strong>{{ listing.bedrooms }}</strong> hab.</span>
        <span v-if="listing.bathrooms != null"><strong>{{ listing.bathrooms }}</strong> baños</span>
      </div>
    </div>
  </article>
</template>
