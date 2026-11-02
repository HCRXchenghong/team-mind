# TeamMind UI 库选型与预览

这是前端重建的选型评审页，**不是业务代码**，不参与构建。

## 打开方式

直接双击 `index.html`（`file://` 即可，无需启动服务）：

```sh
open docs/ui-preview/index.html
```

所有组件包已下载到 `vendor/`，离线可用。

## 内容

| 文件 | 说明 |
| --- | --- |
| `index.html` | 选型报告：需求画像、实时预览、加权评分、能力矩阵、推荐与落地步骤 |
| `demos/shell.css` | 六个 demo 共用的外壳（侧栏、顶栏、KPI、表格容器），保证对照组一致 |
| `demos/ant-design-vue.html` | Ant Design Vue 4.2.6，真实 UMD 渲染 |
| `demos/element-plus.html` | Element Plus 2.14.7，真实 UMD 渲染 |
| `demos/naive-ui.html` | Naive UI 2.45.3，真实 UMD 渲染 |
| `demos/tdesign.html` | TDesign Vue Next 1.20.10，真实 UMD 渲染 |
| `demos/arco.html` | Arco Design Vue 2.59.0（旧技术栈对照），真实 UMD 渲染 |
| `demos/shadcn-vue.html` | shadcn-vue 无 UMD 包，用等价标记静态复刻视觉语言 |

每个真实 demo 都支持右上角切换亮/暗主题、筛选、搜索和“新建任务”弹窗。

## vendor 来源与许可

均为 MIT 许可，版本固定，便于复现：

- `vue@3.5.44`、`dayjs@1.11.19`（含 antd UMD 所需插件）
- `ant-design-vue@4.2.6`
- `element-plus@2.14.7`
- `naive-ui@2.45.3`
- `tdesign-vue-next@1.20.10`
- `@arco-design/web-vue@2.59.0`

UMD 包仅用于本地预览，重建时请通过 npm 安装并按需引入，不要把这些文件拷进业务工程。
