// Package icons 内嵌产品图标，供桌面菜单入口安装时写入。
package icons

import _ "embed"

// AppIconPNG 是 256×256、带圆角透明背景的应用图标。
//
//go:embed appicon-256.png
var AppIconPNG []byte
