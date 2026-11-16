# zero-beat-core

共享 Go 业务模块，路径 `github.com/sheyiyuan/zero-beat/zero-beat-core`。

- 公开 `app`、`store`、`source`、`player`、`sync`、`media`、`keymap` 等业务包；TUI 直接使用这些包，不跨模块引用 internal。
- `media` 只提供引擎输入、状态与事件；音频输出实现由调用方提供。
- `keymap` 提供稳定动作、TOML 校验与通用键表示；Bubble Tea 转换位于 TUI。
- `sync` 共用快照、加密与三方合并逻辑，支持系统 Git 和 go-git。

本模块目前保留原有业务 API。完整 headless Session、GUI RPC 和 Android 下载处理器后续交付；不能把编译可用当作移动端功能已完成。

在仓库根运行 `make check`；`make check-modules` 用临时模块代理检查独立消费。下载功能当前仍保留桌面 ffmpeg 行为。
