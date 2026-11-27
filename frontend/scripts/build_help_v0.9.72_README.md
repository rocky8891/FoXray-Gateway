# 帮助说明索引生成 v0.9.72

适用完整脚本：`frontend/scripts/build_help_v0.9.72.mjs`。使用 Node.js 24 与同一 package-lock.json 安装的依赖。

```sh
npm --prefix frontend ci
node frontend/scripts/build_help_v0.9.72.mjs
npm --prefix frontend run build
```

构建自动执行索引生成器。脚本从各页面的 SettingInfo、Field hint/info、页面说明提取文字，生成 `frontend/src/help_catalog.json`；包含动态分支的说明同时收录不同条件下的文字，实时值以原页面为准。通用表格的自动列宽说明集中维护在索引中。帮助中心同时加载 `help_articles.json` 的完整使用文档。

索引 ID 由标题和内容摘要生成，避免增加条目导致收藏错位。新加的独立 SettingInfo 若无法提取作者提供的说明会中断构建；通用 UI 包装组件由其调用位置提供实际说明，不单独当作空文档。源文件按名称排序，构建结果可复现。

TypeScript 5.9.3 以只用于 AST 解析的开发依赖别名 typescript-ast 固定安装；产品类型检查仍明确运行项目 TypeScript 7 工具，不使用可能被别名覆盖的 tsc 命令。旧版本脚本和手册保留在 Git 历史中。

文档分类按标题、内容和所属模块判断，网络与路由、Xray、WireGuard、DNS/DDNS 等独立展示；分类顺序统一维护在 `frontend/src/help_categories.json`，人工手册的类别同步维护在 `help_articles.json`。
