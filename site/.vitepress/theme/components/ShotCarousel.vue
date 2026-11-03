<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { withBase } from 'vitepress'

interface Shot {
  src: string
  alt: string
  title: string
  caption: string
}

const shots: Shot[] = [
  {
    src: '/images/library.webp',
    alt: '曲库界面：左栏歌单，右栏曲目，底部常驻歌词与播放条',
    title: '曲库',
    caption: '左栏歌单，右栏曲目，底部常驻歌词与播放条',
  },
  {
    src: '/images/menu.webp',
    alt: '歌曲操作菜单：下一首播放、加入队列、加入歌单、下载、移除、标记',
    title: '操作菜单',
    caption: '右键歌曲：下一首播放、加入队列 / 歌单、下载、移除、标记',
  },
  {
    src: '/images/lyrics.webp',
    alt: '歌词页：逐行歌词与当前句高亮',
    title: '歌词',
    caption: '逐行歌词，当前句渐变高亮，整体偏移可调',
  },
  {
    src: '/images/queue.webp',
    alt: '队列页：列表循环、单曲、随机与顺序四种模式',
    title: '队列',
    caption: '列表循环 / 单曲 / 随机 / 顺序，支持混源队列',
  },
  {
    src: '/images/search.webp',
    alt: '本地搜索：在当前歌单中搜索',
    title: '本地搜索',
    caption: '按 / 搜当前歌单，20 条结果可切换歌单 / 全部',
  },
  {
    src: '/images/search-source.webp',
    alt: '音源搜索：gumi 的网易云搜索结果',
    title: '音源搜索',
    caption: '按 ? 搜音源，支持单源与合并结果',
  },
  {
    src: '/images/sync.webp',
    alt: '同步页：git 仓库、SSH/HTTPS、自动同步与冲突入口',
    title: '同步',
    caption: 'SSH / HTTPS、自动同步、冲突处理与归档',
  },
]

const track = ref<HTMLElement | null>(null)
const active = ref(0)
let onResize: (() => void) | null = null

function prefersReducedMotion() {
  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function scrollToShot(index: number, smooth = true) {
  const el = track.value
  if (!el) return
  const target = Math.max(0, Math.min(shots.length - 1, index))
  const slide = el.children[target] as HTMLElement | undefined
  if (!slide) return
  el.scrollTo({
    left: slide.offsetLeft - el.offsetLeft,
    behavior: smooth && !prefersReducedMotion() ? 'smooth' : 'auto',
  })
  active.value = target
}

function go(index: number) {
  scrollToShot(index)
}

function prev() {
  if (active.value > 0) scrollToShot(active.value - 1)
}

function next() {
  if (active.value < shots.length - 1) scrollToShot(active.value + 1)
}

function onScroll() {
  const el = track.value
  if (!el) return
  const children = Array.from(el.children) as HTMLElement[]
  if (!children.length) return
  const center = el.scrollLeft + el.clientWidth / 2
  let nearest = 0
  let best = Number.POSITIVE_INFINITY
  children.forEach((child, index) => {
    const childCenter = child.offsetLeft - el.offsetLeft + child.clientWidth / 2
    const distance = Math.abs(childCenter - center)
    if (distance < best) {
      best = distance
      nearest = index
    }
  })
  active.value = nearest
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowLeft') {
    event.preventDefault()
    prev()
  } else if (event.key === 'ArrowRight') {
    event.preventDefault()
    next()
  } else if (event.key === 'Home') {
    event.preventDefault()
    go(0)
  } else if (event.key === 'End') {
    event.preventDefault()
    go(shots.length - 1)
  }
}

onMounted(() => {
  onResize = () => scrollToShot(active.value, false)
  window.addEventListener('resize', onResize)
})

onBeforeUnmount(() => {
  if (onResize) window.removeEventListener('resize', onResize)
})
</script>

<template>
  <section
    class="shot-carousel"
    role="region"
    aria-roledescription="carousel"
    aria-label="界面截图"
  >
    <div class="viewport">
      <div
        ref="track"
        class="track"
        tabindex="0"
        aria-label="界面截图，使用左右方向键切换"
        @scroll.passive="onScroll"
        @keydown="onKeydown"
      >
        <figure
          v-for="(shot, index) in shots"
          :key="shot.src"
          class="slide"
          :aria-label="`第 ${index + 1} 张，共 ${shots.length} 张：${shot.title}`"
        >
          <img
            :src="withBase(shot.src)"
            :alt="shot.alt"
            width="1299"
            height="720"
            :loading="index === 0 ? 'eager' : 'lazy'"
            draggable="false"
          />
          <figcaption>
            <span class="shot-title">{{ shot.title }}</span>
            <span class="shot-caption">{{ shot.caption }}</span>
          </figcaption>
        </figure>
      </div>

      <button
        type="button"
        class="nav prev"
        :disabled="active === 0"
        aria-label="上一张截图"
        @click="prev"
      >
        <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
          <path d="M15 5l-7 7 7 7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
      <button
        type="button"
        class="nav next"
        :disabled="active === shots.length - 1"
        aria-label="下一张截图"
        @click="next"
      >
        <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
          <path d="M9 5l7 7-7 7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </div>

    <div class="dots" aria-label="选择截图">
      <button
        v-for="(shot, index) in shots"
        :key="shot.src"
        type="button"
        class="dot"
        :class="{ active: active === index }"
        :aria-label="`第 ${index + 1} 张：${shot.title}`"
        :aria-current="active === index ? 'true' : undefined"
        @click="go(index)"
      />
    </div>
  </section>
</template>

<style scoped>
.shot-carousel {
  margin: 24px 0 8px;
}

.viewport {
  position: relative;
}

.track {
  display: flex;
  gap: 16px;
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  scrollbar-width: none;
  border-radius: 12px;
  outline: none;
}

.track::-webkit-scrollbar {
  display: none;
}

.track:focus-visible {
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 3px;
}

.slide {
  flex: 0 0 100%;
  scroll-snap-align: start;
  margin: 0;
  border: 1px solid var(--vp-c-divider);
  border-radius: 12px;
  overflow: hidden;
  background: var(--vp-c-bg-alt);
}

.slide img {
  display: block;
  width: 100%;
  height: auto;
  margin: 0;
  border-radius: 0;
  aspect-ratio: 1299 / 720;
  object-fit: cover;
  user-select: none;
}

.slide figcaption {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 4px 10px;
  padding: 10px 14px;
  border-top: 1px solid var(--vp-c-divider);
  font-size: 13px;
  line-height: 1.6;
  color: var(--vp-c-text-2);
}

.shot-title {
  font-weight: 600;
  color: var(--vp-c-text-1);
}

.nav {
  position: absolute;
  top: 42%;
  z-index: 2;
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  padding: 0;
  color: var(--vp-c-text-1);
  background: color-mix(in srgb, var(--vp-c-bg) 88%, transparent);
  border: 1px solid var(--vp-c-divider);
  border-radius: 50%;
  box-shadow: 0 4px 16px rgb(0 0 0 / 16%);
  cursor: pointer;
  transition: background 0.2s, opacity 0.2s, transform 0.2s;
}

.nav:hover:not(:disabled) {
  background: var(--vp-c-bg);
  transform: translateY(-1px);
}

.nav:disabled {
  opacity: 0.28;
  cursor: default;
}

.prev {
  left: 14px;
}

.next {
  right: 14px;
}

.dots {
  display: flex;
  justify-content: center;
  gap: 2px;
  padding-top: 10px;
}

.dot {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  padding: 0;
  background: transparent;
  border: 0;
  cursor: pointer;
}

.dot::after {
  content: '';
  width: 8px;
  height: 8px;
  background: var(--vp-c-divider);
  border-radius: 50%;
  transition: background 0.2s, transform 0.2s;
}

.dot.active::after {
  background: var(--vp-c-brand-1);
  transform: scale(1.25);
}

.dot:focus-visible {
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: -4px;
  border-radius: 50%;
}

@media (max-width: 640px) {
  .nav {
    width: 34px;
    height: 34px;
  }

  .prev {
    left: 8px;
  }

  .next {
    right: 8px;
  }

  .slide figcaption {
    padding: 8px 10px;
    font-size: 12px;
  }
}
</style>
