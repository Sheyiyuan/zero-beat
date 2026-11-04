---
layout: home

hero:
  name: zero-beat · 零拍
  text: B 站 + 网易云音乐终端播放器
  tagline: v0.6.1 已就绪：登录态过期自动冻结歌单，单曲音量补偿与自动响度均衡；在同一个终端里搜索、收藏、排队、播放两个平台的音频，带逐行歌词。
  image:
    src: /assets/icons/appicon-round.svg
    alt: zero-beat 应用图标
  actions:
    - theme: brand
      text: 快速上手
      link: /guide/quickstart
    - theme: alt
      text: 安装
      link: /guide/install
    - theme: alt
      text: GitHub
      link: https://github.com/Sheyiyuan/zero-beat

features:
  - icon: >-
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 14h3a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-7a9 9 0 0 1 18 0v7a2 2 0 0 1-2 2h-1a2 2 0 0 1-2-2v-3a2 2 0 0 1 2-2h3"/></svg>
    title: 双源登录与搜索
    details: B 站与网易云各自扫码登录，单源或合并搜索，混源队列，本地歌单可混装两平台曲目。
  - icon: >-
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><rect width="18" height="14" x="3" y="5" rx="2" ry="2"/><path d="M7 15h4m4 0h2M7 11h2m4 0h4"/></g></svg>
    title: 逐行歌词
    details: 平台歌词优先、LRCLIB 兜底，底部常驻当前句并渐变高亮，偏移可调并记住。
  - icon: >-
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83z"/><path d="M2 12a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 12"/><path d="M2 17a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 17"/></g></svg>
    title: 混合歌单
    details: 一个歌单引用多个歌单，再追加自己的曲目；引用源会跟着更新，自己的改动不会动到原歌单。
  - icon: >-
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M12 15V3m9 12v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 10l5 5l5-5"/></g></svg>
    title: 下载与本地优先
    details: 界面里批量下载、统一管理，命令行也能下；断点续传，无损封装并写好歌曲信息，已下载的歌优先播本地。
  - icon: >-
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M15 6a9 9 0 0 0-9 9V3"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/></g></svg>
    title: 跨设备同步
    details: 歌单、键位、注释与加密登录态同步到你自己的 git 仓库，冲突时逐项选保留哪边，自动同步默认开启。
  - icon: >-
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 5H3m9 14H3M14 3v4m2 10v4m5-9h-9m9 7h-5m5-14h-7m-6 5v4m0-2H3"/></svg>
    title: 系统媒体集成
    details: Linux 下接入系统媒体面板与媒体键；封面只在面板里显示，不占终端。
  - icon: >-
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M12 22a1 1 0 0 1 0-20a10 9 0 0 1 10 9a5 5 0 0 1-5 5h-2.25a1.75 1.75 0 0 0-1.4 2.8l.3.4a1.75 1.75 0 0 1-1.4 2.8z"/><circle cx="13.5" cy="6.5" r=".5" fill="currentColor"/><circle cx="17.5" cy="10.5" r=".5" fill="currentColor"/><circle cx="6.5" cy="12.5" r=".5" fill="currentColor"/><circle cx="8.5" cy="7.5" r=".5" fill="currentColor"/></g></svg>
    title: 多彩主题
    details: 两套内置配色各有深浅变体，还能用 theme.toml 逐项覆盖颜色；快捷键全部可自定义。
  - icon: >-
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M11 4.7a.7.7 0 0 0-1.2-.5L6.4 7.6A1.4 1.4 0 0 1 5.4 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.4a1.4 1.4 0 0 1 1 .4l3.4 3.4a.7.7 0 0 0 1.2-.5z"/><path d="M16 9a5 5 0 0 1 0 6m3.4 3.4a9 9 0 0 0 0-12.8"/></g></svg>
    title: 音量与响度
    details: 全局与单曲 dB 补偿，自动响度均衡可选。
  - icon: >-
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 19h8M4 17l6-6l-6-6"/></svg>
    title: 为终端而生
    details: 为终端打造，支持鼠标和命令面板。
---

<div class="release-note">
  <strong>v0.6.1 已就绪</strong>
  <span>登录态过期冻结、单曲音量补偿与自动响度均衡</span>
  <a href="./changelog/v0.6.1.html">发布说明 →</a>
</div>

<img src="/assets/readme/hero.svg" alt="zero-beat 零拍：两个平台，为你同频" style="width:100%;border-radius:12px" />

## 一行安装

```bash
curl -fsSLO https://github.com/Sheyiyuan/zero-beat/releases/latest/download/install.sh
sh install.sh && zero
```

还需要系统 `ffmpeg` 负责音频解码。官方支持 Linux 和 macOS，Arch 有 AUR 包 `zero-beat-git`；Windows 提供了 zip 包，需要自己准备好 ffmpeg 和终端环境。

## 界面

<ShotCarousel />

## 文档地图

- 用户先看：[安装](/guide/install) → [快速上手](/guide/quickstart) → [按键速查](/reference/keys)
- 功能专题：[曲库与歌单](/guide/library)、[播放与歌词](/guide/playback)、[下载](/guide/download)、[跨设备同步](/guide/sync)、[系统媒体](/guide/sysmedia)、[主题与配色](/guide/theme)
- 配置参考：[自定义快捷键](/reference/keymap)、[CLI 子命令](/reference/cli)
- 上手前建议读：[已知限制](/guide/limitations)、[数据与隐私](/guide/privacy)

## 免责声明

本项目仅供学习、研究与技术交流，不用于商业用途。B 站与网易云的音频、视频、封面、商标等内容版权归对应平台及权利人所有，本项目不托管任何音源内容；使用可能遇到账号风控、封禁、接口失效或数据丢失，风险自担。完整条款见[数据与隐私](/guide/privacy#免责声明)。

许可证：[AGPL-3.0](https://github.com/Sheyiyuan/zero-beat/blob/main/LICENSE)。
