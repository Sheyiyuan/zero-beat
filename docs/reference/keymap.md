# 自定义快捷键

快捷键配置是 `~/.config/zero-beat/keymap.toml`。生成一份包含全部默认值的配置：

```bash
zero keymap --init
zero keymap --check
```

全部动作共 59 个，生成配置后即可看到完整列表。

## 示例

```toml
[keys]
play_pause = ["space"]
next       = ["]"]
previous   = ["["]
help_popup  = ["H"]
local_search = ["/"]
search = ["?"]
login       = ["L"]
login_image = ["o"]
download          = ["w"]
download_selected = ["W"]
select            = ["z", "ctrl+space"]
select_all        = ["ctrl+a"]
sync              = ["ctrl+s"]
```

## 规则

- 只写想改的动作，未写的继续用默认值；空数组表示禁用该动作。
- 只支持单键和常见命名键（`space`、`up`、`pgup`、`ctrl+u`、`ctrl+l` 等），不支持 `g g` 序列。
- 文本输入、`enter`/`esc`、`Tab` / `Shift+Tab` 和 `1–9` 视图直达键固定，不参与自定义。
- `H` 弹窗、底部提示和右上角 `L` 会显示实际生效的键位。

## 冲突处理

你写的键永远优先。若它和某个默认键撞了，默认绑定的那一条会被禁用，启动时用黄色提示说明是哪个动作让位，其余自定义照常生效；只有**两个都是你自己写的动作**抢同一个键时，才整份拒绝并回退默认（避免一键双触发）。

- 例：旧配置里 `logout = ["W"]`，`W` 会保留给你的退出登录，默认的「下载选中 = W」被禁用并提示；想两者都要就改其中一个键。
- 例：`next = ["p"]`，`p` 归 `next`，默认的播放模式 `p` 被禁用。

`download_selected` 默认 `W`，`logout` 默认 `ctrl+w`（登录页仍兼容旧 `W` / `X`）。

## 版本迁移

- 升级到 v0.5 后 `j` / `k` / `h` / `l` 恢复为方向移动与左右焦点，歌词改为单键 `T`（`ctrl+l` 仍可用）；用旧版 `--init` 生成的配置不需要改，仍会正常加载。
- v0.6 新增动作 `sync`（默认 `ctrl+s`）；`1–9` 仍然只覆盖前九个页面，同步页用 `Tab` 或 `ctrl+s` 进入。

设计取舍见 [ADR 0012](../adr/0012-configurable-keymap.md)。默认键位全表见[按键速查](keys.md)。
