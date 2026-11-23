# ClassRelay · 课递

在同一局域网上发送课堂文字、网址和多行代码。教师启动一个可执行程序，师生使用浏览器，运行时的界面、代码语法和数据储存均在本地。

## 云端模式

云端模式支持教师账号登录和固定学生链接。每次开课生成新口令，结束课堂撤销学生会话并保留历史。部署步骤见 [云端版部署说明](docs/cloud.md)。

## 立即使用

从 `bin/` 选择对应系统和架构的可执行文件。

| 操作系统 | 文件 |
| --- | --- |
| macOS（Apple 芯片） | `classrelay-darwin-arm64` |
| macOS（Intel） | `classrelay-darwin-amd64` |
| Windows（x64） | `classrelay-windows-amd64.exe` |
| Windows（ARM64） | `classrelay-windows-arm64.exe` |
| Linux（x64 / ARM64） | `classrelay-linux-amd64` / `classrelay-linux-arm64` |

例如在 Apple 芯片 Mac 上，从项目根目录运行：

```sh
./bin/classrelay-darwin-arm64
```

Windows 可双击对应的 `.exe`，或在 PowerShell 中执行 `.\bin\classrelay-windows-amd64.exe`。请保持程序运行。程序自动打开默认浏览器，教师也可使用终端显示的本次启动登录链接。

1. 打开「邀请学生」，选择可被学生访问的局域网网卡。将二维码或学生访问地址和六位课堂口令提供给学生。
2. 学生用浏览器填写姓名和口令。教师在「加入申请」中批准或拒绝。同名学生拥有独立会话。
3. 选择「全班广播」或一名学生发送消息。选择「代码」并指定语言即可高亮，收到消息后可以点「复制」。文本用 Enter 发送、Shift + Enter 换行，代码用 Ctrl / ⌘ + Enter 发送。
4. 学生发送的消息会显示在各自的私聊。教师看到未读数后可进入会话回复。「已送达」表示接收客户端已确认取得消息。教师打开对应私聊后确认取得学生消息。
5. 关闭教师网页后，可以用同一浏览器访问 `http://127.0.0.1:17888/teacher` 回到课堂。停止学生访问可用「邀请学生 → 停止服务」。退出程序可在终端按 Ctrl+C。

广播以发送时已获批准的学生为接收范围，包含离线学生。学生重新连接后会同步断线期间的消息。学生会话依赖浏览器 Cookie 与访问地址，换浏览器、清理 Cookie 或切换教师 IP 后需要重新申请。局域网 HTTP 未加密，请在可信网络使用，避免发送敏感信息。教师的一次性登录链接仅限教师本人使用。

## 从源码构建

需要 Go 1.27.3 或更新版本、Node.js 22.13+ 或 24+、npm、Make 和 rsync。

```sh
make install
make build
./bin/classrelay
```

`make build` 构建前端、同步静态资源到 `web/dist` 并嵌入 Go 可执行文件。`make release` 生成上表中的六个平台文件。`make check` 运行 ESLint、TypeScript 检查和 Go vet。`make test` 运行 Go 集成测试，其中包含 50 个模拟 SSE 客户端。

如需在 Windows PowerShell 直接从源码构建：

```powershell
npm --prefix frontend ci
npm --prefix frontend run build
Copy-Item frontend/dist/* web/dist -Recurse -Force
New-Item -ItemType Directory -Path bin -Force | Out-Null
$env:CGO_ENABLED = "0"
go build -trimpath -o bin/classrelay.exe ./cmd/classrelay
```

## 开发模式

```sh
go run ./cmd/classrelay -no-browser -data-dir /tmp/classrelay-dev-data
```

另开终端，在 `frontend` 执行 `npm run dev`。将 Go 终端给出的登录链接端口改为 `5173`（保留路径和 `#key=…`），即可使用教师界面的热更新。为了同时开发学生端，启动 Vite 前为 `CLASSRELAY_STUDENT_TARGET` 设置学生服务器源地址，例如 `http://192.168.1.42:17887`，并用 `http://127.0.0.1:5173/student` 访问。

教师和学生默认分别使用 TCP 17888 和 17887。用 `-teacher-port`、`-student-port` 更改端口，用 `-data-dir` 指定独立数据目录。每个程序实例使用自己的数据目录。查看 [局域网排查与数据说明](docs/usage.md)。
