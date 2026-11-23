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

## 部署在 Linux 云服务器

使用可以从教室访问的域名，为域名配置 DNS 和 HTTPS 证书。下面以已有 Nginx HTTPS 站点和支持 `LoadCredential` 的 systemd 为例。把模板中的 `relay.example.com` 换成自己的域名。

构建机运行：

```sh
make install
make release
```

按云服务器架构选择 `bin/classrelay-linux-amd64` 或 `bin/classrelay-linux-arm64`，上传并安装为 `/usr/local/bin/classrelay`。上传 [systemd 模板](../deploy/classrelay-cloud.service) 到 `/etc/systemd/system/classrelay-cloud.service`。用安全的终端编辑器将教师密码写入 `/etc/classrelay/teacher-password`，设置为 root 所有、权限 `0600`。密码文件只包含密码，允许末尾有换行。教师账号通过服务文件中的 `-cloud-user` 设置。 [systemd 凭证配置](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html#Credentials)

把 [Nginx 配置片段](../deploy/nginx-classrelay.conf) 放入该域名的 HTTPS `server` 块。配置片段覆盖代理头并关闭响应缓冲，学生端可以通过 SSE 实时接收消息。Go 程序只监听 `127.0.0.1:17888`，浏览器走反向代理的 HTTPS 入口。保持 Nginx 传递的 Host 和 `-cloud-url` 一致。 [Nginx 代理模块](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)

修改服务文件和 Nginx 后，在服务器运行：

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now classrelay-cloud
sudo nginx -t
sudo systemctl reload nginx
```

从教室先打开 `/student` 确认可访问，再登录 `/teacher`，用两台设备完成一次签到、消息收发、结束课堂和换人检查。服务问题可查看 `sudo journalctl -u classrelay-cloud`。

## 数据与账号

- 模板使用 `/var/lib/classrelay` 保存 SQLite。修改密码文件后重启服务，教师用新密码登录。
- 服务重启后保留历史和正在进行的课堂。教师登录会话保存在进程内，重启后应重新登录。仍在同一堂课且保有有效 Cookie 的学生可以恢复。
- 为备份数据，先停止课递服务，再复制完整数据目录，最后重新启动服务。
- 局域网启动方式见 [README](../README.md)。局域网与云端默认使用独立的 SQLite 目录。手动指定 `-data-dir` 时请继续为两个模式使用不同目录。
