# 网站迁回主仓库

2026-11-16 将原独立仓库 `Sheyiyuan/zero-beat-site` 的网站源码纳入 `Sheyiyuan/zero-beat`，路径保持为 `zero-beat-site/`，移除 site 的 Git 子模块注册。

- 导入提交：`3135ab12f132ad2115638265e847f2c00b8af912`。
- 原网站的 11 个受版本管理文件均已保留或在迁移中更新。
- 原仓库没有额外分支、标签、Release 或未关闭的 issue。
- 删除远端前，在执行迁移的主仓库本机保存了完整历史：`.git/site-migration/zero-beat-site.bundle`，并通过 `git bundle verify`。该备份属于本机 Git 元数据，不随 clone 分发；主仓库提交保存本站源码。

后续网站源码在主仓库维护，TUI 用户文档和 GUI 设计文档分别在组件仓库维护。网站构建读取主仓库锁定的组件提交，公开站点地址保持不变。
