# Vue Flow Typedocs Index

更新时间：2026-05-19

项目当前依赖：`@vue-flow/core@^1.48.2`，`@vue-flow/node-resizer@^1.5.1`。

官方入口：[Vue Flow Typedocs](https://vueflow.dev/typedocs/)

## 当前画布重点

本项目画布主要文件：

- `frontend/src/components/CanvasWorkspace.vue`
- `frontend/src/styles.css`

当前主要使用的 Vue Flow 能力：

- `VueFlow` 组件承载节点、边、视口和交互。
- `useVueFlow('canvas-flow')` 获取实例方法，例如 `setViewport`、`zoomTo`、`getNodes`、`removeSelectedElements`。
- 自定义 `node-canvas` slot 渲染业务节点。
- `Handle` + `Position` 定义输入/输出连接点。
- `NodeResizer` 给分组框 resize。
- `ConnectionMode.Strict`、`isValidConnection` 做连接约束。
- `defaultEdgeOptions`、`connectionLineOptions`、`MarkerType.ArrowClosed` 做连线样式。
- `edge-update` 处理边重连。

## 核心组件

| 名称 | 用途 | 文档 |
| --- | --- | --- |
| `VueFlow` | 主画布组件，接收 `nodes`、`edges`、交互 props 和事件 | [FlowProps](https://vueflow.dev/typedocs/interfaces/FlowProps.html) |
| `Handle` | 节点连接点，配合 `sourceHandle` / `targetHandle` | [HandleProps](https://vueflow.dev/typedocs/interfaces/HandleProps.html) |
| `NodeResizer` | 节点/分组尺寸调整，来自 `@vue-flow/node-resizer` | [Node Resizer Docs](https://vueflow.dev/guide/components/node-resizer.html) |

## 常用 Props

### 数据

| Prop | 说明 |
| --- | --- |
| `nodes` | 当前渲染的节点数组 |
| `edges` | 当前渲染的边数组 |
| `defaultViewport` | 初始视口 `{ x, y, zoom }` |
| `defaultEdgeOptions` | 统一配置新边/默认边样式 |
| `connectionLineOptions` | 拖拽连线过程中的线条样式 |

参考：[FlowProps](https://vueflow.dev/typedocs/interfaces/FlowProps.html)

### 交互

| Prop | 说明 |
| --- | --- |
| `minZoom` / `maxZoom` | 缩放上下限 |
| `nodesDraggable` | 是否允许拖动节点 |
| `panOnDrag` | 是否拖拽画布平移 |
| `zoomOnScroll` | 滚轮是否缩放 |
| `selectNodesOnDrag` | 框选时是否选中节点 |
| `selectionKeyCode` | 多选/框选快捷键配置 |
| `panActivationKeyCode` | 按键激活平移，例如 Space |
| `deleteKeyCode` | 删除快捷键；本项目设为 `null`，由业务自己处理 |
| `connectOnClick` | 点击 handle 后可再点击目标 handle 完成连接 |
| `connectionMode` | 连接模式，常用 `ConnectionMode.Strict` |
| `isValidConnection` | 拖拽连接时实时校验是否合法 |
| `edgesUpdatable` | 是否允许重连边 |
| `nodesFocusable` / `edgesFocusable` | 节点/边是否可聚焦 |
| `elevateNodesOnSelect` / `elevateEdgesOnSelect` | 选中时提升层级 |

参考：[FlowProps](https://vueflow.dev/typedocs/interfaces/FlowProps.html)

## 数据类型

| 类型 | 用途 | 文档 |
| --- | --- | --- |
| `Node` | Vue Flow 节点结构，含 `id`、`type`、`position`、`data`、`style`、`parentNode`、`extent` 等 | [Node](https://vueflow.dev/typedocs/interfaces/Node.html) |
| `Edge` | Vue Flow 边结构，含 `source`、`target`、`sourceHandle`、`targetHandle`、`markerEnd`、`animated` 等 | [Edge](https://vueflow.dev/typedocs/interfaces/Edge.html) |
| `Connection` | 新建连接时的临时结构 | [Connection](https://vueflow.dev/typedocs/interfaces/Connection.html) |
| `NodeChange` | 节点变化事件载荷，用于同步选择态等 | [NodeChange](https://vueflow.dev/typedocs/types/NodeChange.html) |
| `NodeDragEvent` | 节点拖动事件载荷 | [NodeDragEvent](https://vueflow.dev/typedocs/interfaces/NodeDragEvent.html) |
| `EdgeMouseEvent` | 边鼠标事件载荷，常用于边右键菜单 | [EdgeMouseEvent](https://vueflow.dev/typedocs/interfaces/EdgeMouseEvent.html) |
| `EdgeUpdateEvent` | 边重连事件载荷 | [EdgeUpdateEvent](https://vueflow.dev/typedocs/interfaces/EdgeUpdateEvent.html) |
| `ViewportTransform` | 视口状态 `{ x, y, zoom }` | [ViewportTransform](https://vueflow.dev/typedocs/interfaces/ViewportTransform.html) |

## 常用事件

| 事件 | 当前用途 |
| --- | --- |
| `connect` | 新增连接，落点后写入业务 `connections` |
| `connect-start` / `connectStart` | 记录起始 handle，用于松手空白处弹出创建节点菜单 |
| `connect-end` / `connectEnd` | 结束连接，决定是否弹出“创建并连接”菜单 |
| `edge-update` / `edgeUpdate` | 边重连后更新业务连接 |
| `nodes-change` | 同步 Vue Flow 选择态到本地 `selectedNodeIDs` |
| `node-drag-stop` | 节点拖动结束后同步坐标并保存历史 |
| `edge-context-menu` | 边右键菜单 |
| `selection-context-menu` | 多选右键菜单 |
| `pane-click` | 点击空白处清理选择/菜单 |
| `viewport-change` | 同步 pan/zoom 到本地 camera |

参考：[FlowProps Events](https://vueflow.dev/typedocs/interfaces/FlowProps.html)

## 实例方法和 Store

通过：

```ts
const flow = useVueFlow('canvas-flow')
```

本项目常用：

| 方法/状态 | 用途 |
| --- | --- |
| `flow.setViewport(viewport, options?)` | 设置平移和缩放 |
| `flow.zoomTo(zoom, options?)` | 缩放到指定倍率 |
| `flow.getNodes.value` | 获取当前 Vue Flow 节点状态，用于拖动后同步位置 |
| `flow.removeSelectedElements()` | 成组后清空 Vue Flow 内部选择 |

参考：[useVueFlow](https://vueflow.dev/typedocs/functions/useVueFlow.html)、[VueFlowStore](https://vueflow.dev/typedocs/interfaces/VueFlowStore.html)

## 枚举

| 枚举 | 常用值 | 用途 | 文档 |
| --- | --- | --- | --- |
| `Position` | `Left`、`Right`、`Top`、`Bottom` | Handle 位置 | [Position](https://vueflow.dev/typedocs/enumerations/Position.html) |
| `ConnectionMode` | `Strict`、`Loose` | 严格区分 source/target handle | [ConnectionMode](https://vueflow.dev/typedocs/enumerations/ConnectionMode.html) |
| `ConnectionLineType` | `SmoothStep`、`Bezier`、`Straight`、`Step` | 拖拽连线样式 | [ConnectionLineType](https://vueflow.dev/typedocs/enumerations/ConnectionLineType.html) |
| `MarkerType` | `Arrow`、`ArrowClosed` | 边箭头样式 | [MarkerType](https://vueflow.dev/typedocs/enumerations/MarkerType.html) |

## 样式入口

项目需要保留：

```css
@import '@vue-flow/core/dist/style.css';
@import '@vue-flow/core/dist/theme-default.css';
@import '@vue-flow/node-resizer/dist/style.css';
```

常覆盖的类名：

| 类名 | 用途 |
| --- | --- |
| `.vue-flow__pane` | 画布背景/鼠标样式 |
| `.vue-flow__node` | Vue Flow 节点外壳 |
| `.vue-flow__node.selected` | 节点选中态 |
| `.vue-flow__edge-path` | 边线条 |
| `.vue-flow__edge.selected` | 边选中态 |
| `.vue-flow__connection-path` | 拖拽中的连接线 |
| `.vue-flow__resize-control` | NodeResizer 控件 |

## 本项目连接规则备忘

业务规则在 `CanvasWorkspace.vue` 的 `canConnect(from, to)`、`outputTypes()`、`acceptedInputTypes()`。

当前语义：

- `prompt` / `llm` 输出 text。
- `image_media` / `image` / `mask` 输出 image。
- `video_media` / `video` 输出 video。
- `audio_media` / `audio` 输出 audio。
- `merge` 输出 merge。
- `image_media` 特判：可连接到 `image`、`video`、`mask`。

Vue Flow 层应调用业务规则，而不是另写一套规则：

```ts
function isValidFlowConnection(connection: Connection) {
  if (!connection.source || !connection.target || connection.source === connection.target) return false
  const from = elementByID(connection.source)
  const to = elementByID(connection.target)
  return Boolean(from && to && canConnect(from, to))
}
```

## 后续扩展方向

- 需要多输入 handle 时，给 `Handle` 设置不同 `id`，并在 `Edge.sourceHandle` / `Edge.targetHandle` 中持久化。
- 需要更强分组能力时，继续使用 `parentNode` + `extent: 'parent'`，不要绕开 Vue Flow 自己的坐标体系。
- 需要键盘操作时，优先用 Vue Flow 的 focus/selection 能力，再接业务快捷键。
- 需要导入导出画布时，持久化业务 `BoardCanvas`，不要直接持久化 Vue Flow runtime node。

