# 开发踩坑记录

AGENTS.md 与 `docs/adr/` 管规范和决策；这里按「现象 → 根因 → 解法 → 教训」记教训本身，供后续开发避免重踩。

## 2026-11 · .eml 导入（对应 ADR 0032）

### 1. 解析截断的附件让导入「悄悄丢文件」

- **现象**：导入的邮件带大附件时，附件打不开，没有任何报错。
- **根因**：`mail.Parse` 为控内存把附件截到 25 MiB，只保留前导切片；上限常量是包内私有的 `maxAttachmentBytes`，调用方无从知道「数据是部分的」。`core` 包另有同值的 `MaxMessageBytes`，两边容易漂移；`core/mail/dkim_security.go` 里又引用了一次私有常量。
- **解法**：导出为 `mail.MaxAttachmentBytes`，注释写明「Size 等于上限即前导切片，不是完整文件」；`dkim_security.go` 改用导出名。导入器据此跳过截断附件的落盘。
- **教训**：跨包共享的上限不要留私有拷贝；「截断」必须让调用方能识别，否则等于静默数据损坏。

### 2. 附件缓存驱逐会毁掉导入邮件的附件

- **现象**：导入完成一段时间后，附件再次打不开。
- **根因**：附件缓存按 2 GiB 预算驱逐最旧文件。普通邮件的附件只是缓存，服务器上还有原本可再下载；导入邮件的附件唯一副本就在缓存里，被驱逐即丢失。
- **解法**：`core.MaxCacheBytes` 提到 4 GiB，注释写明「导入邮件的附件只在这里」。
- **教训**：同一份数据有两种来源（可再取 / 不可再取）时，驱逐策略要按来源区分，或为不可再取的留足余量。

### 3. 本地邮件的修改排队成 ops 会永远重试

- **现象**：移动或标记一封导入的邮件后，操作队列里出现推不出去的 ops。
- **根因**：ops 队列假设每封邮件都有服务器位置（文件夹 + UIDVALIDITY + UID）；本地邮件没有，flags/move 的 op 找不到可写的对面。
- **解法**：`target.onServer`（`server_folder_id IS NOT NULL`）为假时不排队 ops（`core/messages.go` 的 `flagWrites` / `moveWrites`）。反向规则同时确立：不能把服务器邮件移入本地文件夹，否则同步会把服务器副本再导回来，等于复制一份。
- **教训**：给「只在一边存在」的数据复用面向双边的队列时，先问「对面有没有这个东西」。

### 4. Windows 与 Unix 判「应用是否在跑」不是一个 API

- **现象**：应用运行时执行离线导入，直接写库导致 SQLite 锁冲突。
- **根因**：数据目录锁的占用检测两边实现不同：Windows 靠独占 `CreateFile` 打开 `godwit.lock`，Unix 靠 `flock`；没有可移植的单一调用。
- **解法**：`cmd/godwit/guardclosed_windows.go` 与 `guardclosed_other.go` 用 build tag 分平台实现同一个 `guardClosed`，离线导入先问锁再开库；两个文件各自的注释写清同一语义。
- **教训**：跨平台的占用检测要成对放在 build tag 文件里，语义写在公共调用点的注释里，谁实现漏了半边都能对出来。

### 5. UI/CLI 不能绕过边界直接进 core

- **现象**：导入逻辑最初想从界面层直接调 mailsync，被 `app/boundary_test.go` 拦下。
- **根因**：架构边界：界面与命令行只经 `core.Service`（本机 API）与平台层活动；`core/mailsync`、`core/store` 是内部包。
- **解法**：新建 `importeml/` 桥接包：UI 与 CLI 都跑 `importeml.Drive`，只有它（和 `core.Service.ImportEml`）触碰 mailsync/store；journal 放数据目录旁，两个入口共享，互相续跑。
- **教训**：两个入口要做同一件事时，把「机制」放进桥接包，「表单/参数」留在各自层——边界测试会替你守住。

### 6. 测试夹具没给 dataDir，journal 落到工作目录

- **现象**：app 层的导入测试把 `import-eml-state.json` 写进了运行目录。
- **根因**：harness 的 fakePlatform 的 `DataDir()` 原本返回空，`journalPath` 便回退到相对路径。
- **解法**：`app/harness_test.go` 显式设置 `h.plat.dataDir = h.env.DataDir`，journal 与「接入 AI 应用」的路径都落进测试数据目录。
- **教训**：平台抽象返回空值时的回退行为，测试一定会踩到——要么让夹具显式填齐，要么让回退路径可预期。
