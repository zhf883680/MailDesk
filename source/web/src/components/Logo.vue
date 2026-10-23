<template>
  <svg :width="size" :height="size" viewBox="0 0 48 48" fill="none" xmlns="http://www.w3.org/2000/svg" aria-label="MailDesk">
    <rect x="2" y="2" width="44" height="44" rx="12" fill="#0f6b5c" />
    <rect x="2" y="2" width="44" height="44" rx="12" fill="url(#mdg)" />
    <defs>
      <linearGradient id="mdg" x1="2" y1="2" x2="46" y2="46" gradientUnits="userSpaceOnUse">
        <stop stop-color="#12816e" />
        <stop offset="1" stop-color="#0a4a40" />
      </linearGradient>
    </defs>
    <path d="M11 18.5C11 16.6 12.6 15 14.5 15h19c1.9 0 3.5 1.6 3.5 3.5v11c0 1.9-1.6 3.5-3.5 3.5h-19c-1.9 0-3.5-1.6-3.5-3.5v-11Z" fill="#f5f1e9" />
    <path d="m12 18 10.7 7.8c.8.6 1.8.6 2.6 0L36 18" stroke="#0f6b5c" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" />
    <path d="M35.5 8.5l1.15 2.85L39.5 12.5l-2.85 1.15L35.5 16.5l-1.15-2.85L31.5 12.5l2.85-1.15L35.5 8.5Z" fill="#e8b04b" />
    <circle cx="40.5" cy="20" r="1.5" fill="#e8b04b" />
  </svg>
</template>

<script setup>
defineProps({ size: { type: Number, default: 28 } })
</script>
