<script setup>
import { computed } from 'vue'

const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 18 },
  stroke: { type: [Number, String], default: 1.7 },
  filled: { type: Boolean, default: false },
})

const icons = {
  inbox: { paths: ['M3 8.8 5.7 4.6A2 2 0 0 1 7.4 3.6h9.2a2 2 0 0 1 1.7 1L21 8.8V17a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8.8Z', 'M3 9.2h5.3l1.3 2.4h4.8l1.3-2.4H21'] },
  layers: { paths: ['m12 3 9 5-9 5-9-5 9-5Z', 'm3 13 9 5 9-5'] },
  grid: { paths: ['M4 4h6v6H4z', 'M14 4h6v6h-6z', 'M4 14h6v6H4z', 'M14 14h6v6h-6z'] },
  settings: { paths: ['M4 7h9', 'M17 7h3', 'M4 17h4', 'M12 17h8'], circles: [[15, 7, 2], [10, 17, 2]] },
  search: { paths: ['M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16Z', 'm21 21-4.3-4.3'] },
  send: { paths: ['m22 2-7 20-4-9-9-4 20-7Z', 'M22 2 11 13'] },
  star: { paths: ['m12 3.6 2.6 5.3 5.9.9-4.3 4.1 1 5.8-5.2-2.7-5.2 2.7 1-5.8L3.5 9.8l5.9-.9L12 3.6Z'] },
  trash: { paths: ['M4 7h16', 'M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2', 'M6.5 7l.8 12.2a2 2 0 0 0 2 1.8h5.4a2 2 0 0 0 2-1.8L17.5 7', 'M10 11v6', 'M14 11v6'] },
  refresh: { paths: ['M20 11a8 8 0 1 0-.7 4.5', 'M20 5v6h-6'] },
  reply: { paths: ['M9 14 4 9l5-5', 'M4 9h9a7 7 0 0 1 7 7v3'] },
  languages: { paths: ['M3 6h9', 'M7.5 6c0 4.5-2 7.5-4.5 9', 'M5 11c1.8 2.6 4 4.2 6.5 5.2', 'm13 21 4-10 4 10', 'M14.6 17.5h4.8'] },
  sparkles: { paths: ['m12 3 1.8 4.7 4.7 1.8-4.7 1.8L12 16l-1.8-4.7L5.5 9.5l4.7-1.8L12 3Z', 'M18.5 16.5 19.3 18.7 21.5 19.5 19.3 20.3 18.5 22.5 17.7 20.3 15.5 19.5 17.7 18.7 18.5 16.5Z'] },
  chevronDown: { paths: ['m6 9 6 6 6-6'] },
  chevronLeft: { paths: ['m15 18-6-6 6-6'] },
  chevronRight: { paths: ['m9 6 6 6-6 6'] },
  close: { paths: ['M18 6 6 18', 'M6 6l12 12'] },
  menu: { paths: ['M4 7h16', 'M4 12h16', 'M4 17h16'] },
  check: { paths: ['m4 12.5 5 5L20 6.5'] },
  alert: { paths: ['M12 3 2.5 20h19L12 3Z', 'M12 9v5', 'M12 17.4v.1'] },
  eye: { paths: ['M2 12s3.6-6.5 10-6.5S22 12 22 12s-3.6 6.5-10 6.5S2 12 2 12Z'], circles: [[12, 12, 2.6]] },
  eyeOff: { paths: ['M3 3l18 18', 'M10.6 6.1A9.9 9.9 0 0 1 12 6c6.4 0 10 6 10 6a17 17 0 0 1-3.2 3.8', 'M6.3 7.8A16.6 16.6 0 0 0 2 12s3.6 6 10 6a9.9 9.9 0 0 0 4-.8'] },
  lock: { paths: ['M6 10V8a6 6 0 0 1 12 0v2', 'M5 10h14v10a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V10Z'] },
  plus: { paths: ['M12 5v14', 'M5 12h14'] },
  logout: { paths: ['M9 20H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h3', 'm15 8 4 4-4 4', 'M9 12h10'] },
  shield: { paths: ['M12 3 4 6.5v5c0 4.6 3.2 8.4 8 9.5 4.8-1.1 8-4.9 8-9.5v-5L12 3Z'] },
  bot: { paths: ['M7 7h10a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2Z', 'M9 4v3', 'M15 4v3', 'M2 11h3', 'M19 11h3', 'M2 15h3', 'M19 15h3', 'M9 18v3', 'M15 18v3'], circles: [[9.5, 12, 1], [14.5, 12, 1]] },
  clock: { paths: ['M12 7v5l3 2'], circles: [[12, 12, 9]] },
  pencil: { paths: ['m4 20 4.5-1 10-10a2.1 2.1 0 0 0-3-3l-10 10L4 20Z', 'm13.5 6.5 3 3'] },
  arrowLeft: { paths: ['M19 12H5', 'm11 18-6-6 6-6'] },
  filter: { paths: ['M4 5h16l-6 7.5V20l-4-2v-5.5L4 5Z'] },
  external: { paths: ['M14 4h6v6', 'M20 4 11 13', 'M18 14v5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h5'] },
  mail: { paths: ['M3 6.5h18v11H3z', 'm3 7 9 6 9-6'] },
  tag: { paths: ['M3 12V5a2 2 0 0 1 2-2h7l9 9-9 9-9-9Z'], circles: [[7.5, 7.5, 1.2]] },
  bell: { paths: ['M18 9a6 6 0 1 0-12 0c0 5-2 6-2 6h16s-2-1-2-6', 'M10.5 20a2 2 0 0 0 3 0'] },
  archive: { paths: ['M3 5h18v4H3z', 'M5 9v10h14V9', 'M10 13h4'] },
  paperclip: { paths: ['M20.5 11.5 12 20a5 5 0 0 1-7.1-7.1l8.6-8.6a3.3 3.3 0 0 1 4.7 4.7l-8.6 8.6a1.7 1.7 0 0 1-2.4-2.4l7.9-7.9'] },
  file: { paths: ['M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8l-5-5Z', 'M14 3v5h5'] },
  download: { paths: ['M12 4v11', 'm7.5 10.5 4.5 4.5 4.5-4.5', 'M4.5 20h15'] },
  lockOpen: { paths: ['M6 10V8a6 6 0 0 1 12 0', 'M5 10h14v10a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V10Z'] },
}

const icon = computed(() => icons[props.name] || icons.mail)
</script>

<template>
  <svg
    :width="size" :height="size" viewBox="0 0 24 24" :fill="filled ? 'currentColor' : 'none'"
    stroke="currentColor" :stroke-width="stroke" stroke-linecap="round" stroke-linejoin="round"
    aria-hidden="true" focusable="false"
  >
    <path v-for="(d, i) in icon.paths" :key="'p' + i" :d="d" />
    <circle v-for="(c, i) in (icon.circles || [])" :key="'c' + i" :cx="c[0]" :cy="c[1]" :r="c[2]" />
  </svg>
</template>
