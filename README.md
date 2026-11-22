# VIID Hub 智能视图库

Go + PostgreSQL + React + shadcn/ui 的视图库首版。工作台展示接入趋势、设备健康、告警和最近抓拍。管理后台支持设备登记、批量导入与启停、人员、人脸、机动车、非机动车记录检索，以及图片上传、预览、下载和关联。

## 快速启动

准备 Go 1.26.2+、Node.js 22.12+ 和一个可连接的 PostgreSQL 数据库。在项目根目录用 PowerShell 执行：

```powershell
if (!(Test-Path .env)) { Copy-Item .env.example .env }
```

在 `.env` 中填写 `DATABASE_URL`（指向已创建的数据库）和 `VIID_DEVICE_PASSWORD`（设备接入密码），然后启动服务。应用会在目标库中创建所需表、视图和索引。

```powershell
.\start.ps1
```

浏览器访问 [本机管理后台](http://127.0.0.1:14000/)。管理用户名和密码默认均为 `admin`，可在 `.env` 中分别通过 `VIID_USER` 和 `VIID_PASSWORD` 修改，重启后台后生效。启动脚本构建前端，再启动 Go 服务，由同一端口提供页面、API 和图片。

### 前端热更新

开发时可在两个终端分别启动 Go 后端和 Vite 前端。两个终端均从仓库根目录开始。

```powershell
go run ./cmd/viid
```

```powershell
cd web
npm ci
npm run dev
```

打开[热更新入口](http://127.0.0.1:14001/)。Vite 将 `/api` 和 `/VIID` 代理至本机 14000。Go 后端的监听地址由 `LISTEN_ADDR` 控制，默认仅绑定本机。设备从另一台机器接入时配置可达的监听地址。跨主机部署使用 HTTPS 反向代理。

## 数据与页面

- 工作台和平台概览可查看最近 24 小时、7 天或 30 天的抓拍趋势。设备健康展示当前状态，运行监控列出离线和心跳超时的设备。
- 设备管理支持登记、编辑、JSON 批量导入、启停、名称/编号/状态筛选以及心跳事件。批量导入接受设备数组，必填字段为 `id` 和 `name`，可选字段为 `kind`、`ip`、`point`、`enabled`。重复编号会报告冲突。
- 人员、人脸和车辆记录支持按采集时间、设备和关键词查询，查看原始字段和关联图片。车辆可按类型、颜色筛选，多个视图共享同一记录。
- 图片资源支持 JPEG、PNG 上传、类型和关联状态筛选、分页、列表/网格切换、记录关联、下载和删除。图像数据管理把图集与人员表并排展示。
- 所有抓拍时间按 UTC+8 展示。图像内容以 PostgreSQL `bytea` 保存，记录保留 JSONB 字段。删除记录会解除图片关联，图片文件仍可查询。
- `go run ./cmd/viid -demo` 或 `.\start.ps1 -Demo` 在设备目录为空时，一次性导入由程序生成的抽象占位 JPEG 和合成记录（不包含真实人员或设备照片）。演示记录编号以 `DEMO-` 开头。演示设备只在导入时产生心跳，5 分钟后会按实际时间显示超时。正常启动不会重复播种。

### 列表操作

- **分页**：表格默认每页 15 条，可选 10、15、20、50 或 100 条。切换条数会回到第一页；在同一浏览器、同一访问地址下，其他列表和刷新后的页面都会沿用上次的选择。
- **查询**：“更多筛选”默认收起。展开后设置条件并点击“查询”，再收起时可从按钮查看已生效的高级条件数，例如“更多筛选（2）”。点击“重置”清除条件。
- **查看详情**：人员和人脸列表优先展示抓拍图、姓名和证件号，机动车列表优先展示抓拍图、车牌和车型。点击记录或“查看”打开右侧详情。也可以用 `Ctrl+K`（macOS 上用 `⌘K`）搜索并直达记录。
- **时间显示**：列表和详情使用 `YYYY-MM-DD HH:mm:ss`。表格较宽时可横向滚动。趋势图的 24 小时刻度显示时分，7 天和 30 天刻度显示月日，悬停数据点可查看完整时间。

## 功能目标与原型验收清单

上游包括抓拍设备、AI 分析平台和下级视图库。中心负责注册、鉴权、保活、校验、去重及图像和结构化数据存储。下游包括上级视图库、第三方业务系统和检索应用。下表保留原型中的未完成项，供后续逐项验收。

| 原型 | 当前功能 | 待完成 |
| --- | --- | --- |
| 1 设备管理 | 登记、编辑、批量导入与启停、在线状态、心跳事件 | 能力配置、级联关系、CPU/内存/网络遥测、远程维护与重启 |
| 2 图片资源 | 缩略图、预览、双时间、检索、关联、导出、上传下载 | 车牌等子图分类、批量上传、跨页批量下载 |
| 3 非机动车 | 车型、颜色、方向、骑行人数、按图查看 | 有效性审核、头盔等扩展字段展示 |
| 4 图像数据管理 | 分类图集、人员检索、关联图片、原始字段 | 图集多选、批量导入、来源和存储状态筛选 |
| 5 机动车 | 车牌、车型、速度、方向、照片和 JSON | 日/周/月统计、标注、布控、品牌与子车型 |
| 6 车辆综合查询 | 双类型切换、设备与时间筛选、列设置 | 号牌颜色和方向筛选、标签、区域及点位定位 |
| 7 人员记录 | 脱敏列表与详情、授权查看证件、脱敏导出、审计 | 批量下载关联原图、轨迹查询、更多证件及采集字段 |
| 8 平台概览 | 周期趋势、设备健康、全库抓拍筛选和分页 | 置信度与处置状态、容量上限和空间告警 |
| 9 工作台 | 接入指标、趋势、告警、可下钻流水与状态栏 | 告警确认、处置、恢复历史和通知 |
| 跨页管理 | 导航搜索、操作审计、订阅与级联投递监控 | 多用户和角色、点位/区域/字典管理、系统配置、接口日志和统计分析 |

### 首版前四项

1. **图片时间**。设备图采用子图抓拍时间，缺失时使用父记录抓拍时间。入库时间取数据库接收时间。手动图片没有抓拍时间，后续关联记录也不会改写它。历史版本混用了两种时间，仅能确认的历史抓拍时间会回填。未知值显示为“—”。图片列表的高级筛选支持切换时间字段，默认按入库时间筛选及排序。
2. **全量查询**。工作台、概览的类型、设备和时段由 PostgreSQL 筛选。“查看更多”把同一组条件带到分页流水。设备选择器支持服务端检索和翻页。运行监控可翻阅所有当前离线或超时设备。
3. **证件号与审计**。管理 API 在列表、详情、原始 JSON、搜索和导出中遮盖 `IDNumber`，含嵌套对象及数组。服务端默认关闭明文查看。如需授权，在本机配置 `VIID_IDENTITY_READER` 为当前管理账号并重启，仅详情里的显式查看可返回明文。操作审计记录详情访问、授权查看或拒绝、导出、原图下载和删除，包含账号、时间与对象编号。此版本为单管理账号。
4. **接入流程**。使用真实 HTTP 连接、临时 PostgreSQL schema 和模拟设备验证注册、校时、保活、四类对象带图上传、重传去重、冲突、整批失败回滚、断线超时后重新注册、补传、注销及停用。当前按模拟环境验收，真机互通未验证。

## 设备接口

设备端支持 HTTP Digest 挑战应答（MD5，qop=auth）。首次请求收到 401，按挑战重新签名并携带与设备编号一致的 `User-Identify`。摘要包含方法、请求 URI 和 nonce 计数。服务端校验时效和重复使用。为了兼容早期设备，默认也接受主动发送的 Basic 凭据，设置 `VIID_DIGEST_ONLY=true` 可仅接受 Digest。设备用户名为设备编号，密码取 `VIID_DEVICE_PASSWORD`。管理后台使用独立账号和 8 小时 HttpOnly 会话。设备上报前须注册并保持在线。停用、注销或超过 5 分钟未保活或交互的设备不能上报；超时设备发送有效心跳即可恢复在线。

| 方法 | 路径 | 请求对象或用途 |
| --- | --- | --- |
| POST | `/VIID/System/Register` | `RegisterObject.DeviceID` |
| POST | `/VIID/System/Keepalive` | `KeepaliveObject.DeviceID` |
| POST | `/VIID/System/UnRegister` | `UnRegisterObject.DeviceID` |
| GET | `/VIID/System/Time` | 返回 `SystemTimeObject.LocalTime` |
| POST / GET | `/VIID/Subscribes` | 四类结构化数据实时订阅、查询本人订阅 |
| PUT | `/VIID/Subscribes/{id}` | 取消本人订阅 |
| POST（出站） | `/VIID/SubscribeNotifications` | 按订阅号发送通知，校验逐条业务回执 |
| POST / GET | `/VIID/Persons` | `PersonListObject.PersonObject[]` |
| POST / GET | `/VIID/Faces` | `FaceListObject.FaceObject[]` |
| POST / GET | `/VIID/MotorVehicles` | `MotorVehicleListObject.MotorVehicleObject[]` |
| POST / GET | `/VIID/NonMotorVehicles` | `NonMotorVehicleListObject.NonMotorVehicleObject[]` |

每条记录需要对应对象编号（例如 `PersonID`）、当前认证设备的 `DeviceID`，以及抓拍时间。时间支持 `yyyyMMddHHmmss` 或 RFC 3339。单批 1–500 条。可附加 `SubImageList.SubImageInfoObject[]`，其中 `Data` 为 JPEG/PNG 的 Base64，`ImageID` 为图片编号。单图上限 8 MiB，整次请求上限 32 MiB。仅含远程地址的图像元数据保留在原始记录中。

一批对象及其内联图片在同一事务内写入。重复发送具有相同编号及相同结构化元数据的对象会返回成功并保留首次写入的图片。编号与已有设备或元数据冲突时返回 409。普通采集端的 GET 查询限定为当前设备数据，支持 `page`、`size`、`q`、`start`、`end`，页大小上限 200。

### PowerShell 联调示例（Basic 兼容模式）

以下命令会在目标库创建一台测试设备和一条人员记录。

```powershell
$settings = Get-Content .env | ConvertFrom-StringData
$device = '50010300001320009999'
$auth = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes("${device}:$($settings.VIID_DEVICE_PASSWORD)"))
$headers = @{ Authorization = "Basic $auth" }
$base = 'http://127.0.0.1:14000/VIID'
$register = @{RegisterObject = @{DeviceID = $device}} | ConvertTo-Json
Invoke-RestMethod "$base/System/Register" -Method Post -Headers $headers -ContentType application/json -Body $register
$now = [DateTimeOffset]::UtcNow.ToOffset([TimeSpan]::FromHours(8)).ToString('yyyyMMddHHmmss')
$record = @{PersonID="TEST-$now"; DeviceID=$device; ShotTime=$now; Name='测试人员'}
$batch = @{PersonListObject=@{PersonObject=@($record)}} | ConvertTo-Json -Depth 6
Invoke-RestMethod "$base/Persons" -Method Post -Headers $headers -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($batch))
Invoke-RestMethod "$base/Persons?page=1&size=10" -Headers $headers
```

## 下游查询、订阅与级联

- 第三方或检索应用在 `Authorization: Bearer <token>` 中发送 `VIID_QUERY_TOKEN`，通过 `GET /exchange/records`、`GET /exchange/images` 按条件分页，通过 `GET /exchange/images/{id}/file` 取原图。记录返回脱敏字段。未配置令牌时关闭消费端访问。
- 在本地 `.env` 中将 `VIID_PEERS_FILE` 指向一个受保护的 JSON 文件，例如 `.runtime/peers.json`。重启后在“订阅与级联”中选择目标、对象类型和可选来源设备。可为上级视图库和第三方各建规则。示例使用本地模拟地址。
- 规则匹配的新上报和投递任务一起提交。重复上报不重复建任务。暂停规则会停止接收新记录并暂停其待投递任务。恢复后继续排队任务。失败自动退避，累计 8 次失败停止，可在页面重新投递。接收方须按 `Idempotency-Key` 或记录编号处理可能重复的网络投递。
- `webhook` 向指定地址 POST 脱敏记录和投递编号，可设置回调专用令牌。`viid` 用独立平台编号向上级注册、校时、保活，按四类对象接口发送脱敏结构化记录和原始内联子图，保留来源设备编号并核对上级业务回执。配置 `notify` 目标可接受来自指定订阅方的 `/VIID/Subscribes`，向登记的 `/VIID/SubscribeNotifications` 发通知。

```json
{
  "测试业务系统": {"mode":"webhook","url":"http://127.0.0.1:18081/events","token":"example-only"},
  "测试上级": {"mode":"viid","url":"http://127.0.0.1:18082","user":"50010300001320009999","password":"example-only"}
}
```

**标准订阅模拟范围**。目标模式 `notify` 需要 `url`（接收端根地址）、`subscriber` 和 `subscriberPassword`（订阅方的入站身份），以及 `user` 和 `password`（本级向接收端发送通知的身份）。两个平台使用各自的 20 位编号，平台编号由 `VIID_SERVER_ID` 配置。订阅者先注册，按订阅类别 11、12、13、14 选人员、人脸、机动车、非机动车。当前每条规则选一类，可按平台或已登记设备订阅，按起止时间生效。通知的目标地址须与预配置接收方一致。取消订阅立即停发待投递任务。已授权、在线的订阅方也可查询这四类记录及单条对象，返回脱敏数据。设备本身仅可查询自己的上报。

## 验证与适用范围

```powershell
$settings = Get-Content .env | ConvertFrom-StringData
$env:VIID_TEST_DATABASE_URL = $settings.DATABASE_URL
go test -count=1 -v ./...
go vet ./...
npm --prefix web run build
```

设置 `VIID_TEST_DATABASE_URL` 后，集成测试会创建独立的临时 schema，验证四类对象上报、图片原文、重复提交、整批回滚、身份验证、图片关联、设备注销与停用，结束后清理测试 schema。该变量为空时会跳过 PostgreSQL 集成测试。服务运行期间可访问[健康检查](http://127.0.0.1:14000/healthz)确认数据库连通性。

**标准范围**。本项目按 GA/T 1400.4—2017 的公共接口及四类结构化对象实现接入子集。注册按附录 B.1 的 Digest 挑战应答核验，注销、保活、校时分别对应 7.2.2–7.2.4。GB/T 46363—2025 是另一个接口规范，需要独立的资源、字段及安全机制验收。当前没有真机或第三方认证报告。参考[行业标准登记](https://peer.example.invalid/hb/search/stdHBDetailed?id=8B1827F25411BB19E05397BE0A0AB44A)和[国家标准登记](https://peer.example.invalid/bzgk/std/newGbInfo?hcno=C2B5678EACB05CBA826A9C8324A6EDF9)。

**协议 TODO**。逐条核对厂商字段字典与必填项、对象编号编码、更完整的查询表达式、多类别和定时订阅、通知历史查询、目录同步。补齐独立图像上传、视频/文件等资源、逐设备密钥、证书认证、运维遥测和标准测试工具的互操作验证。URL-only 图片仅保留来源元数据，当前不会主动下载。

**2026-11-21 本地验收**。两套隔离数据库 schema 的 HTTP 集成测试、`go vet ./...` 和前端生产构建均通过。Chrome 已验证筛选后下钻、设备搜索、翻页、两类图片时间、脱敏 JSON 导出、审计、告警分页与数据交换入口。独立的 curl Digest 客户端也完成了本机校时挑战应答。本轮验收入口为 [14002](http://127.0.0.1:14002/)，项目默认启动端口仍为 14000。

本地验收包括 `TestPostgresLifecycle` 和 `TestExchangeOverHTTP`。后者启动两个使用独立 schema 的视图库、业务回调与通知接收端，核对 Digest 挑战和重复 nonce 计数拒绝、订阅和取消、双库带图级联、消费端鉴权和脱敏、503 后同一投递编号重试。测试不需要真实设备、生产口令或可访问的上级平台。

项目目录、代码风格、Windows 工具约定和提交说明见[贡献指南](AGENTS.md)。
