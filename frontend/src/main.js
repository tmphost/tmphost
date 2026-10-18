import { createWebHistory } from 'vue-router'
import { createtmphostApp } from './app'

const { app } = createtmphostApp({
  history: createWebHistory(window.__TMPHOST_BASE_PATH__ || '/'),
  platform: 'web',
})
app.mount('#app')
