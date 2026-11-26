# 课递云端版

云端版提供一个教师账号、一间同时进行的课堂和一个长期学生入口。师生都通过公网 HTTPS 访问。已结束的课堂和消息保存在云服务器。

| 入口 | 示例 |
| --- | --- |
| 教师登录 | `https://relay.example.com/teacher` |
| 学生固定链接 | `https://relay.example.com/student` |

## 上课流程

1. 教师登录，输入课堂名称并点「开始上课」。
2. 在「邀请学生」中查看长期链接、二维码和本节课六位口令。学生收藏同一个链接，每节课填自己的姓名和当堂口令，等待教师批准。
3. 教师点「结束课堂」并确认后，系统撤销本节学生会话。仍打开网页的学生会回到等候页。教师可在「上课记录」中切换历史课堂和会话。
4. 同一节课中，共用电脑换人时，点「更换学生 → 退出并重新签到」。更换设备的学生可在新设备重新申请，旧设备的会话可由学生退出或由教师移除。同名申请分别审批。

同一节课内，浏览器刷新可以恢复当前身份。课堂结束后，学生即使还保留旧 Cookie，也只有重新签到并获批后才能加入下一节课。

## 在 Linux 云服务器上直接运行

在构建机执行以下命令。

```sh
make install
make release
```

按服务器架构选择 `bin/classrelay-linux-amd64` 或 `bin/classrelay-linux-arm64`，上传到一个专用目录，命名为 `classrelay`。进入该目录，创建权限为 `0600` 的 `teacher-password` 文件，写入 12–256 字节的教师密码。文件只包含密码，末尾可有换行。默认教师账号是 `teacher`，可用 `-cloud-user` 修改。

配置域名和 HTTPS，将 [Nginx 配置片段](../deploy/nginx-classrelay.conf) 放入该域名的 HTTPS `server` 块。配置片段负责传递公网 Host 和 HTTPS 协议，并关闭响应缓冲以支持 SSE。Go 程序默认监听 `127.0.0.1:17888`。 [Nginx 代理模块](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)

在程序目录中执行以下命令，把示例域名换成自己的域名。

```sh
chmod +x ./classrelay
./classrelay -cloud-url https://relay.example.com
```

程序在终端前台运行，按 Ctrl+C 停止。需要退出 SSH 后继续运行时，可以在 tmux 会话里执行同一条启动命令。

## 数据与账号

- 云端模式以执行命令时所在的目录为准，密码读取 `./teacher-password`，SQLite 保存到 `./data/classrelay.sqlite`。可用 `-cloud-password-file` 和 `-data-dir` 指定其他路径。
- 旧版若使用过云端默认目录 `ClassRelayCloud`，升级时可继续用 `-data-dir` 指向原目录。也可以在停止旧进程后，把完整数据目录复制到新目录下的 `data`。此前通过 systemd 模板部署的数据库位于 `/var/lib/classrelay`。
- 服务重启后保留历史和正在进行的课堂。教师登录会话保存在进程内，重启后应重新登录。仍在同一堂课且保有有效 Cookie 的学生可以恢复。
- 复制目录前先停止程序。保留完整的 `data` 目录和教师密码文件即可在新位置继续运行。
