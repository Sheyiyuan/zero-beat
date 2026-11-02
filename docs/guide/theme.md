# 主题

两套内置配色，每套都有深色 / 浅色两个变体：

| 配色 ID | 名称 | 说明 |
| --- | --- | --- |
| `zero-red` | 佐洛红 zero red | 默认。取自应用图标的红、粉、炭黑与米白 |
| `hanpai-blue` | 半拍蓝 hanpai blue | v0.6 及更早版本的蓝色配色 |

默认模式是 `auto`：读 `COLORFGBG`，背景色号为 7 或 9–15 时用浅色，其它或读不到时用深色。

## 选择模式与配色

```bash
zero theme                     # 查看当前模式、配色与自定义文件
zero theme auto|dark|light     # 设置深浅模式
zero theme palette             # 查看当前配色和可选值
zero theme palette zero-red    # 佐洛红（默认）
zero theme palette hanpai-blue # 半拍蓝
```

临时覆盖只影响这次启动，优先级高于数据库设置：

```bash
ZERO_THEME=light zero
ZERO_THEME_PALETTE=hanpai-blue zero
```

## 自定义颜色

`zero theme --init` 生成 `~/.config/zero-beat/theme.toml`。模板里的每一项都是注释，取消注释想覆盖的项即可：

```toml
[dark]
accent = "#ff8080"
sel_bg = "#ff5555"

[light]
accent = "#b02a52"
```

（示例里是默认值，改成自己想要的颜色即可。）

- `[dark]` / `[light]` 分别作用于深色 / 浅色变体；只写想改的项，未写的继续跟随所选配色。
- 颜色写法：`#rgb` 或 `#rrggbb`。
- 未知槽位、未知段名、非法颜色会让 `zero theme --check` 报错；启动时配置无效会回退到内置配色并在状态行提示。
- 自定义是覆盖层，不改变 `zero-red` / `hanpai-blue` 的选择；模板注释里的色值是佐洛红的默认值，仅作参考。

槽位：

| 槽位 | 作用 |
| --- | --- |
| `accent` | 标题、快捷键、进度条已播放部分 |
| `text` | 正文 |
| `muted` | 次要文字 |
| `dim` | 分隔线、占位、未播放进度 |
| `ok` / `err` / `warn` | 成功 / 错误 / 警告 |
| `sel_bg` / `sel_fg` | 选中行（含光标行）背景 / 文字 |
| `play_bg` / `play_fg` | 正在播放行、歌词焦点行背景 / 文字 |
| `mark_bg` / `mark_fg` | 多选标记背景 / 文字 |

改完重启终端界面生效，改的过程中可以随时用 `zero theme --check` 校验。
