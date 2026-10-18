import { defineStore } from 'pinia'

const PLATFORMS = ['web', 'desktop']

// The supported seam for behavior only one build can offer.
export const usePlatformStore = defineStore('platform', {
  state: () => ({ platform: 'web' }),
  getters: {
    isDesktop: (s) => s.platform === 'desktop',
    isWeb: (s) => s.platform === 'web',
  },
  actions: {
    setPlatform(next) {
      this.platform = PLATFORMS.includes(next) ? next : 'web'
    },
  },
})
