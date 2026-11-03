// 站点主题 = VitePress 默认主题 + 佐洛红色板（见 custom.css）
import DefaultTheme from 'vitepress/theme'
import ShotCarousel from './components/ShotCarousel.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('ShotCarousel', ShotCarousel)
  },
}
