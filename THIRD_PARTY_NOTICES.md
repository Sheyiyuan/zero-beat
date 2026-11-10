# Third-party notices

zero-beat 以 AGPL-3.0 发布。下面列出随二进制分发的依赖，以及运行时需要的外部程序与许可。

## go-musicfox/netease-music

- 版本：v1.6.0
- 许可：MIT
- 用途：网易云 weapi/cloudsearch、二维码登录 chainId、`song_url_v1` 音质、歌词与歌单协议。
- 项目：https://github.com/go-musicfox/netease-music

```text
Copyright (c) 2011-2017 GitHub Inc.

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

## UnblockNeteaseMusic

- 版本：v0.1.5（`github.com/go-musicfox/UnblockNeteaseMusic`，通过 replace 替换上游）
- 许可：GPL-3.0
- 状态：作为 `netease-music` 的传递依赖编译，但 zero-beat 显式设置
  `util.UNMSwitch = false`，并对取流设置 `SkipUNM = true`，**不启用版权替换**。
- 项目：https://github.com/go-musicfox/UnblockNeteaseMusic
- 许可证全文：https://www.gnu.org/licenses/gpl-3.0.html

## FFmpeg

- 版本：由用户安装的系统 `ffmpeg` 决定，本项目不随包分发
- 许可：由构建选项决定。上游默认 LGPL-2.1-or-later；启用 `--enable-gpl` 为 GPL-2.0-or-later；再叠加 `--enable-version3` 为 GPL-3.0-or-later（仅启用 `--enable-version3` 时为 LGPL-3.0-or-later）。部分可选组件另有许可。
- 版本与许可因发行版而异，例如 Debian 的 ffmpeg 是 GPL-2.0-or-later；以本机 `ffmpeg -L` 的输出为准。
- 用途：解码音频流并输出 s16le PCM；下载时用 `-c copy` 封装并写入标签
- 项目：https://ffmpeg.org/
- 说明：本项目只以子进程方式调用，不链接 FFmpeg 库；本项目自身代码仍按 AGPL-3.0 发布。分发二进制或安装包时应自行确认所用 FFmpeg 的许可与来源；若把 FFmpeg 一并打包，需同时满足 FFmpeg 的许可。

## BurntSushi/toml

- 版本：v1.4.0
- 许可：MIT
- 用途：加载 `~/.config/zero-beat/keymap.toml`。
- 项目：https://github.com/BurntSushi/toml

## godbus/dbus

- 版本：v5.1.0
- 许可：BSD-2-Clause
- 用途：Linux 会话 D-Bus，MPRIS 系统媒体面板与媒体键。
- 项目：https://github.com/godbus/dbus

## dhowden/tag

- 版本：v0.0.0-20240417053706-3d75831295e8
- 许可：BSD-3-Clause
- 用途：读取本地音频的 ID3v2 / FLAC / MP4 / Ogg 标签与内嵌歌词（本地音乐库扫描与本地歌词识别）。
- 项目：https://github.com/dhowden/tag

## golang.org/x/crypto、golang.org/x/term、golang.org/x/text

- 版本：x/crypto v0.53.0、x/term v0.44.0、x/text v0.38.0
- 许可：BSD-3-Clause（Go Authors）
- 用途：同步口令的 Argon2id / HKDF / XChaCha20-Poly1305，CLI 无回显读取口令，以及本地歌词文本的 Unicode 归一化与 GB18030 解码。
- 项目：https://pkg.go.dev/golang.org/x/crypto 、https://pkg.go.dev/golang.org/x/term 、https://pkg.go.dev/golang.org/x/text

## 其他

其余 Go 依赖及版本见 [go.mod](go.mod)；许可信息随各模块分发。
