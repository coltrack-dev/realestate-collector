<script setup lang="ts">
defineProps<{ total: number }>()
</script>

<template>
  <header class="site-header">
    <a class="site-brand" href="/" aria-label="Territorio, datos inmobiliarios de Colombia">
      <span class="site-mark" aria-hidden="true"><i></i><i></i><i></i><i></i></span>
      <span class="site-brand-copy"><strong>territorio</strong><small>datos inmobiliarios</small></span>
    </a>

    <nav class="site-nav" aria-label="Navegación principal">
      <a class="site-nav-link is-active" href="#catalogo">Propiedades</a>
      <a class="site-nav-link" href="#mapa">Mapa</a>
      <a class="site-nav-link" href="#estadisticas">Estadísticas</a>
    </nav>

    <div class="site-context">
      <span class="site-context-label">Contexto</span>
      <span class="site-context-place"><span class="site-context-dot"></span>Colombia</span>
      <span class="site-context-divider" aria-hidden="true"></span>
      <span class="site-context-count"><strong>{{ total }}</strong> activos</span>
    </div>
  </header>
</template>
