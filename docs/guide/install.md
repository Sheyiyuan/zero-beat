# 安装与依赖

## 依赖

- **Go 1.26.10+**（仅源码构建需要）
- **ffmpeg**（负责音频解码）
- 音频输出：Linux 用 PulseAudio / PipeWire，macOS 用 CoreAudio
- 可选：系统 `git`（跨设备同步需要）；Linux 会话 D-Bus（系统媒体面板需要）
- 网易云协议层随程序内置，无需额外安装

## 平台支持

官方支持 Linux / macOS。Windows 提供了 zip 包，需要自己准备好 ffmpeg 和终端环境，欢迎提交 PR。

## Linux / macOS（安装脚本）

```bash
curl -fsSLO https://github.com/Sheyiyuan/zero-beat/releases/latest/download/install.sh
sh install.sh
```

默认安装到 `~/.local/bin`。如果该目录不在 `PATH` 里，脚本会识别登录 shell（bash / zsh / fish / sh）并写入对应的配置文件，免去手动改环境变量；不想改动配置文件可加 `--no-path`。也可指定版本/前缀：

```bash
VERSION=v0.6.1 sh install.sh
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

## Windows

Release 里提供 zip 包（交叉编译产物），解压后需要自己准备好 ffmpeg 和终端环境。欢迎 PR。

## 桌面菜单入口（Linux）

安装脚本在 Linux 图形会话里会自动执行 `zero desktop install`：把 `.desktop` 入口（`~/.local/share/applications/zero-beat.desktop`）和 256×256 圆角图标装进 `~/.local/share`，应用菜单/启动器里显示的就是产品图标；`Terminal=true` 由桌面环境负责拉起终端。

以 `sudo` 安装时脚本会跳过应用菜单注册，需要改用普通用户执行 `zero desktop install`。源码构建同样可以手动执行：

```bash
zero desktop install      # 注册入口（含图标）
zero desktop uninstall    # 移除入口
```

用 `sudo` 安装时脚本会跳过自动注册（否则入口会写进 root 的家目录）；装完用普通用户执行一次 `zero desktop install` 即可。

## 下一步

装好后去[快速上手](quickstart.md)看首次启动与扫码登录。
