# Windows x64 发布候选

## 从同一份源码生成候选包

准备独立、固定的源码副本，包含 `go.mod`、`go.sum`、`cmd/viid` 和同版本 `web` 源码及锁文件。记录 Git 提交号；如果候选含未提交改动，同时保存构建输入文件的 SHA-256 清单。使用 Go 1.26.2+、Node.js 22.12+ 和已填充的本机依赖缓存。以下命令在该副本根目录的 PowerShell 中执行。缓存缺失时构建将在相应步骤停止。

```powershell
$ErrorActionPreference = 'Stop'
$env:GOTOOLCHAIN = 'local'
$env:GOWORK = 'off'
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$env:npm_config_offline = 'true'

$out = Join-Path $env:TEMP ('viid-win-x64-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $out | Out-Null
npm.cmd --prefix web ci --offline --no-audit --no-fund
if ($LASTEXITCODE) { throw '前端锁定依赖安装失败' }
npm.cmd --prefix web run build
if ($LASTEXITCODE) { throw '前端构建失败' }
go build -mod=readonly -trimpath -buildvcs=false -o (Join-Path $out 'viid.exe') ./cmd/viid
if ($LASTEXITCODE) { throw 'Go 构建失败' }

$web = New-Item -ItemType Directory -Path (Join-Path $out 'web')
Copy-Item -LiteralPath 'web/dist' -Destination $web.FullName -Recurse
$zip = "$out.zip"
Compress-Archive -LiteralPath (Join-Path $out 'viid.exe'), $web.FullName -DestinationPath $zip
Get-FileHash -Algorithm SHA256 -LiteralPath $zip
```

将 ZIP 解压到另一个空目录。根目录应有 `viid.exe` 和 `web/dist/index.html`。逐项确认首页引用的 JS 和 CSS 都在 `web/dist` 下，且与构建输出的 SHA-256 相同。用 `go version -m .\viid.exe` 核对服务模块及 Windows/amd64 构建标记。交付时同时记录源码标识、工具版本和 ZIP 摘要。仓库的 `start.ps1` 会现场构建前端并调用 `go run`，用于开发启动。

## 工作目录和配置

在目标机将一个版本完整解压到固定目录。将进程托管器的可执行文件设为该目录下的 `viid.exe`，工作目录设为该目录本身。服务通过相对路径 `web/dist` 提供前端，并在启动时向配置的数据库执行内嵌 schema。由运维在首次启动前核对目标库和变更授权，向运行账户注入配置。

| 用途 | 配置键 |
| --- | --- |
| 数据库连接、设备认证 | `DATABASE_URL`、`VIID_DEVICE_PASSWORD` |
| 后台登录 | `VIID_USER`、`VIID_PASSWORD` |
| 监听地址、平台编号 | `LISTEN_ADDR`、`VIID_SERVER_ID` |
| 严格模型校验、Digest 认证 | `VIID_STRICT_2017`、`VIID_DIGEST_ONLY` |
| 按需启用的交换配置、消费端令牌、身份查看权限 | `VIID_PEERS_FILE`、`VIID_QUERY_TOKEN`、`VIID_IDENTITY_READER` |

显式设置后台账号和口令；上线前核对严格校验与 Digest 策略。使用交换功能时，配置文件采用运行账户可读的绝对路径。凭据与配置文件由运维独立交付。常规启动直接运行 EXE，省略演示数据参数。

## 日志和启动排查

进程托管器应分别保留标准输出、标准错误，记录每次启动的时间、版本摘要、工作目录、PID，以及退出时间和退出码。按启动批次保存日志，保留上一次退出记录。共享排查材料前移除凭据和业务数据。

- **没有监听。** 对照配置的端口检查监听 PID，再查同一启动批次的标准错误和进程退出事件。启动横幅在实际绑定端口前输出。可据后续错误区分端口占用、配置错误或数据库初始化失败；缺少退出证据时记录为原因未知。
- **端口已监听。** 在获准启动的环境检查 `GET /healthz` 的 HTTP 状态和数据库状态，再访问 `/` 及首页引用的 JS、CSS。健康接口通过但页面缺失时，核对进程工作目录和该版资源清单。
- **协议请求被拒。** 用响应头 `X-Request-ID` 对照标准错误中的 `request_id`，结合 HTTP 状态、阶段、批次条目和事务结果定位。正常 Digest 握手的首次挑战可能没有请求编号。
