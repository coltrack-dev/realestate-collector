<script setup lang="ts">
withDefaults(defineProps<{ total: number; activeTab?: 'properties' | 'map' | 'stats' }>(), { activeTab: 'properties' })
const emit = defineEmits<{ navigate: [tab: 'properties' | 'map' | 'stats'] }>()
</script>

<template>
  <header class="site-header">
    <a class="site-brand" href="/" aria-label="Territorio, datos inmobiliarios de Colombia">
      <span class="site-mark" aria-hidden="true"><i></i><i></i><i></i><i></i></span>
      <span class="site-brand-copy"><strong>territorio</strong><small>datos inmobiliarios</small></span>
    </a>

    <nav class="site-nav" aria-label="Navegación principal">
      <button class="site-nav-link" :class="{ 'is-active': activeTab === 'properties' }" type="button" @click="emit('navigate', 'properties')">Propiedades</button>
      <button class="site-nav-link" :class="{ 'is-active': activeTab === 'map' }" type="button" @click="emit('navigate', 'map')">Mapa</button>
      <button class="site-nav-link" :class="{ 'is-active': activeTab === 'stats' }" type="button" @click="emit('navigate', 'stats')">Estadísticas</button>
    </nav>

    <div class="site-context">
      <span class="site-context-label">Contexto</span>
      <span class="site-context-place"><span class="site-context-dot"></span>Colombia</span>
      <span class="site-context-divider" aria-hidden="true"></span>
      <span class="site-context-count"><strong>{{ total }}</strong> activos</span>
    </div>
  </header>
</template>
