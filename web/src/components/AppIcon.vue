<template>
  <svg
    class="np-icon"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    :stroke="solid ? 'none' : 'currentColor'"
    :fill="solid ? 'currentColor' : 'none'"
    :stroke-width="strokeWidth"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path v-for="(d, i) in paths" :key="i" :d="d" />
    <circle
      v-for="(c, i) in circles"
      :key="'c' + i"
      :cx="c[0]"
      :cy="c[1]"
      :r="c[2]"
    />
    <rect
      v-for="(r, i) in rects"
      :key="'r' + i"
      :x="r[0]"
      :y="r[1]"
      :width="r[2]"
      :height="r[3]"
      :rx="r[4] || 0"
    />
  </svg>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 18 },
  strokeWidth: { type: [Number, String], default: 1.8 },
})

// 24x24 feather 风格线性图标
const ICONS = {
  grid: { r: [[3, 3, 7, 7, 1], [14, 3, 7, 7, 1], [3, 14, 7, 7, 1], [14, 14, 7, 7, 1]] },
  heart: { p: ['M20.8 5.6a5 5 0 0 0-7.1 0L12 7.3l-1.7-1.7a5 5 0 1 0-7.1 7.1l8.8 8.8 8.8-8.8a5 5 0 0 0 0-7.1z'] },
  heartFill: { p: ['M20.8 5.6a5 5 0 0 0-7.1 0L12 7.3l-1.7-1.7a5 5 0 1 0-7.1 7.1l8.8 8.8 8.8-8.8a5 5 0 0 0 0-7.1z'], solid: true },
  star: { p: ['M12 3l2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1L3.2 9.5l6.1-.9L12 3z'] },
  upload: { p: ['M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4', 'M17 8l-5-5-5 5', 'M12 3v13'] },
  download: { p: ['M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4', 'M7 10l5 5 5-5', 'M12 15V3'] },
  server: { r: [[2, 3, 20, 7, 2], [2, 14, 20, 7, 2]], p: ['M6 6.5h.01', 'M6 17.5h.01'] },
  search: { c: [[11, 11, 7]], p: ['M20 20l-3.5-3.5'] },
  sun: { c: [[12, 12, 4]], p: ['M12 2v2', 'M12 20v2', 'M4.9 4.9l1.4 1.4', 'M17.7 17.7l1.4 1.4', 'M2 12h2', 'M20 12h2', 'M4.9 19.1l1.4-1.4', 'M17.7 6.3l1.4-1.4'] },
  moon: { p: ['M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z'] },
  menu: { p: ['M3 6h18', 'M3 12h18', 'M3 18h18'] },
  logout: { p: ['M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4', 'M16 17l5-5-5-5', 'M21 12H9'] },
  close: { p: ['M18 6L6 18', 'M6 6l12 12'] },
  prev: { p: ['M15 18l-6-6 6-6'] },
  next: { p: ['M9 18l6-6-6-6'] },
  info: { c: [[12, 12, 9]], p: ['M12 16v-4', 'M12 8h.01'] },
  trash: { p: ['M3 6h18', 'M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2', 'M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6', 'M10 11v6', 'M14 11v6'] },
  refresh: { p: ['M21 12a9 9 0 1 1-2.6-6.4', 'M21 3v6h-6'] },
  plus: { p: ['M12 5v14', 'M5 12h14'] },
  check: { p: ['M20 6L9 17l-5-5'] },
  image: { r: [[3, 3, 18, 18, 2]], c: [[8.5, 8.5, 1.5]], p: ['M21 15l-5-5L5 21'] },
  video: { p: ['M23 7l-7 5 7 5V7z'], r: [[1, 5, 15, 14, 2]] },
  zoomIn: { c: [[11, 11, 7]], p: ['M20 20l-3.5-3.5', 'M11 8v6', 'M8 11h6'] },
  zoomOut: { c: [[11, 11, 7]], p: ['M20 20l-3.5-3.5', 'M8 11h6'] },
  reset: { p: ['M3 12a9 9 0 1 0 3-6.7', 'M3 4v5h5'] },
  layers: { p: ['M12 2l9 5-9 5-9-5 9-5z', 'M3 12l9 5 9-5', 'M3 17l9 5 9-5'] },
  user: { c: [[12, 8, 4]], p: ['M4 21a8 8 0 0 1 16 0'] },
  devices: { r: [[6, 2, 12, 20, 2]], p: ['M11 18h2'] },
  sliders: { p: ['M4 21v-7', 'M4 10V3', 'M12 21v-9', 'M12 8V3', 'M20 21v-5', 'M20 12V3', 'M1 14h6', 'M9 8h6', 'M17 16h6'] },
  folder: { p: ['M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z'] },
  play: { p: ['M6 4l14 8-14 8V4z'] },
  playFill: { p: ['M7 4.5v15l13-7.5-13-7.5z'], solid: true },
  eye: { p: ['M1.5 12S5 5 12 5s10.5 7 10.5 7-3.5 7-10.5 7S1.5 12 1.5 12z'], c: [[12, 12, 3]] },
  eyeOff: { p: ['M17.9 17.9A10.6 10.6 0 0 1 12 19c-7 0-10.5-7-10.5-7a18.4 18.4 0 0 1 5.1-5.9', 'M9.9 4.7A10.9 10.9 0 0 1 12 4.7c7 0 10.5 7.2 10.5 7.2a18.6 18.6 0 0 1-2.2 3.2', 'M14.1 14.1a3 3 0 1 1-4.2-4.2', 'M2 2l20 20'] },
  sparkle: { p: ['M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8L12 3z'] },
  calendar: { r: [[3, 5, 18, 16, 2]], p: ['M16 3v4', 'M8 3v4', 'M3 11h18'] },
  chevronLeft: { p: ['M15 18l-6-6 6-6'] },
  chevronRight: { p: ['M9 18l6-6-6-6'] },
  panel: { r: [[3, 3, 18, 18, 2]], p: ['M9 3v18'] },
  db: { p: ['M12 8c4.4 0 8 1.3 8 3s-3.6 3-8 3-8-1.3-8-3 3.6-3 8-3z', 'M4 11v5c0 1.7 3.6 3 8 3s8-1.3 8-3v-5', 'M20 11v5'] },
  broom: { p: ['M19 4l-7 7', 'M14 6l4 4', 'M12 11l-6 6', 'M6 17l-2 4 4-2'] },
  bolt: { p: ['M13 2L4 14h7l-1 8 9-12h-7l1-8z'] },
  expand: { p: ['M4 9V4h5', 'M20 9V4h-5', 'M4 15v5h5', 'M20 15v5h-5'] },
  compress: { p: ['M9 4v5H4', 'M15 4v5h5', 'M9 20v-5H4', 'M15 20v-5h5'] },
  album: { r: [[3, 5, 18, 15, 2]], p: ['M3 9h9', 'M12 20l3-4 2.5 3L20 16.5V20H12z'] },
  settings: { c: [[12, 12, 3]], p: ['M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-2.9 1.2 2 2 0 1 1-4 0 1.7 1.7 0 0 0-2.9-1.2l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A1.7 1.7 0 0 0 3 15a2 2 0 1 1 0-4 1.7 1.7 0 0 0 1.2-2.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1A1.7 1.7 0 0 0 10 4.6a2 2 0 1 1 4 0 1.7 1.7 0 0 0 2.9 1.2l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1A1.7 1.7 0 0 0 21 11a2 2 0 1 1 0 4h-1.6z'] },
  shield: { p: ['M12 2l8 3v6c0 5-3.4 9.3-8 11-4.6-1.7-8-6-8-11V5l8-3z'] },
  key: { c: [[8, 8, 4]], p: ['M10.8 12.2L21 2l1 1-1.6 1.6 2 2-2.4 2.4-2-2-3 3 1 3-2.4 2.4-2.6-2.6-2.6 2.6'] },
}

const def = computed(() => ICONS[props.name] || {})
const paths = computed(() => def.value.p || [])
const circles = computed(() => def.value.c || [])
const rects = computed(() => def.value.r || [])
const solid = computed(() => def.value.solid === true)
</script>

<style scoped>
.np-icon {
  display: block;
  flex-shrink: 0;
  color: currentColor;
}
</style>
