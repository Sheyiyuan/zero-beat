# 安装与依赖

## 依赖

- **Go 1.26.10+**（仅源码构建需要；Go 1.21+ 默认的 GOTOOLCHAIN=auto 会自动下载对应工具链）
- **ffmpeg**（负责解码 AAC / HE-AAC / FLAC）
- 可用的音频输出：Linux 上 PulseAudio/PipeWire，macOS CoreAudio（Windows 未适配，仅提供交叉编译产物）
- 可选：系统 `git`（跨设备同步需要）；Linux 会话 D-Bus（系统媒体面板需要）
- 网易云协议层：`go-musicfox/netease-music`（Go module 自动拉取，无需额外系统依赖）

## 平台支持

官方支持 Linux / macOS。Windows 会提供交叉编译的 zip，但不做适配、不维护、不保证可用；需要的用户请自行判断和处理，欢迎提交 PR。

## Linux / macOS（安装脚本）

```bash
curl -fsSLO https://github.com/Sheyiyuan/zero-beat/releases/latest/download/install.sh
sh install.sh
```

默认安装到 `~/.local/bin`。如果该目录不在 `PATH` 里，脚本会识别登录 shell（bash / zsh / fish / sh）并写入对应的配置文件，免去手动改环境变量；不想改动配置文件可加 `--no-path`。也可指定版本/前缀：

```bash
VERSION=v0.6.0 sh install.sh
sudo sh install.sh --prefix /usr/local
```

脚本会校验 release 的 SHA256，并检查 `ffmpeg`；缺失时打印对应包管理器的安装命令。卸载用 `sh install.sh --uninstall`，会同时移除脚本写入的 PATH 配置与桌面菜单入口。

## Arch Linux（AUR）

仓库内 `.aur/aur-repo` 维护 AUR 打包，包名 `zero-beat-git`（跟随 main 分支源码构建），可用常见 AUR 助手安装。

## 从源码

```bash
git clone https://github.com/Sheyiyuan/zero-beat && cd zero-beat
make build          # 产出 bin/zero
./bin/zero          # 进终端界面
```

## Windows（不维护）

Release 里会附带 `zero_<version>_windows_amd64.zip`，但没有做 Windows 适配，不保证可用；解压后需要自行处理 `ffmpeg` 与终端环境。欢迎 PR。

## 桌面菜单入口（Linux）

安装脚本在 Linux 图形会话里会自动执行 `zero desktop install`：把 `.desktop` 入口和圆角图标装进 `~/.local/share`，应用菜单/启动器里显示的就是产品图标；`Terminal=true` 由桌面环境负责拉起终端。源码构建同样可以手动执行：

```bash
zero desktop install      # 注册入口（含图标）
zero desktop uninstall    # 移除入口
```

用 `sudo` 安装时脚本会跳过自动注册（否则入口会写进 root 的家目录）；装完用普通用户执行一次 `zero desktop install` 即可。

## 下一步

装好后去[快速上手](quickstart.md)看首次启动与扫码登录。
