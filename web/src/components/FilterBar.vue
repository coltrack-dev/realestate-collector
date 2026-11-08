<script setup lang="ts">
import { reactive } from 'vue'
import type { ListingFilters } from '../types'

const emit = defineEmits<{ apply: [filters: ListingFilters]; reset: [] }>()

const filters = reactive<ListingFilters>({
  source: '', city: '', operation: '', property_type: '',
  min_price: '', max_price: '', min_area: '', max_area: '', limit: 50, offset: 0,
})

function submit() {
  emit('apply', { ...filters, offset: 0 })
}

function reset() {
  Object.assign(filters, { source: '', city: '', operation: '', property_type: '', min_price: '', max_price: '', min_area: '', max_area: '', offset: 0 })
  emit('reset')
}
</script>

<template>
  <form class="filter-panel" @submit.prevent="submit">
    <div class="filter-heading">
      <div>
        <span class="eyebrow">Búsqueda</span>
        <h2>Encuentra tu próximo lugar</h2>
      </div>
      <button class="clear-button" type="button" @click="reset">Limpiar</button>
    </div>

    <label class="field field-wide">
      <span>Ciudad o zona</span>
      <input v-model="filters.city" type="text" placeholder="Bogotá D.C., Medellín…" />
    </label>

    <div class="field-grid">
      <label class="field"><span>Fuente</span><select v-model="filters.source"><option value="">Todas</option><option value="vixora">VIXORA</option><option value="dccolombia">D.C Colombia</option></select></label>
      <label class="field"><span>Operación</span><select v-model="filters.operation"><option value="">Todas</option><option value="SALE">Venta</option><option value="RENT">Arriendo</option></select></label>
      <label class="field"><span>Tipo</span><select v-model="filters.property_type"><option value="">Todos</option><option value="APARTMENT">Apartamento</option><option value="HOUSE">Casa</option><option value="FINCA">Finca</option><option value="LOTE">Lote</option><option value="COMMERCIAL">Comercial</option><option value="OFFICE">Oficina</option></select></label>
    </div>

    <div class="filter-section">
      <span class="filter-label">Rango de precio · COP</span>
      <div class="range-grid"><input v-model="filters.min_price" type="number" min="0" placeholder="Desde" /><input v-model="filters.max_price" type="number" min="0" placeholder="Hasta" /></div>
    </div>
    <div class="filter-section">
      <span class="filter-label">Área construida · m²</span>
      <div class="range-grid"><input v-model="filters.min_area" type="number" min="0" step="0.1" placeholder="Mínimo" /><input v-model="filters.max_area" type="number" min="0" step="0.1" placeholder="Máximo" /></div>
    </div>
    <button class="primary-button" type="submit"><span>Aplicar filtros</span><span class="button-arrow">↗</span></button>
  </form>
</template>
