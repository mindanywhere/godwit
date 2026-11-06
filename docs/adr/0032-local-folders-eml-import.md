---
status: accepted
---

# 本地文件夹：.eml 备份导入为本机邮件

**.eml 备份（邮箱导出）可以直接导入 Godwit，落在一个只在本机的「本地文件夹」里：不与服务器对账，不参与同步，永远不会被发出去。** 需求来自把旧邮箱导出的备份搬进 Godwit：备份已经在硬盘上，不该为了读它再把成千上万封信上传回服务器。两条路线里选了本地：一条是用 IMAP APPEND 把 .eml 传回服务器再同步回来（`tools/emlimport` 验证过，网易服务器要 ID 命令这类方言），一条是直接进本机库。本地路线更快、没有服务器方言问题，数据也不出这台机器。

**模型。** `folders` 新增 `local` 列（迁移默认 0，老库升级即得）。`local = 1` 的文件夹是 Godwit 自己的，服务器上没有同名文件夹，`syncFolders` 既不同步它，也不因 LIST 里没有而判为 stale 删除（folders.go 只对 `local = 0` 判 stale）。里面的邮件没有服务器位置（`server_folder_id` 等列为 NULL），这正是同步对「等待移动认领」邮件的既有语义：线程合并（`planThread`）、标签指纹（`labelFP`）、搜索、快照全部沿用，不为导入另起一套。

**领域规则。**

- 导入目标文件夹与服务器文件夹同名时拒绝：后来同步会把它当服务器文件夹认领，本机邮件和服务器副本混在一起。
- 不能把邮件移入本地文件夹（`Move` 拒绝）：服务器副本会被落在这里，同步又会把它导回原处，等于变相复制。
- 本地邮件的标记（已读、星标）和移动只在本机生效：`flagWrites` / `moveWrites` 只对有服务器位置（`target.onServer`）的邮件排队 ops，否则队列里会出现永远推不出去、或把「服务器上没有的副本」推给服务器的操作。
- 去重按文件夹进行：有标签指纹（`label_fp`，覆盖没有 Message-ID 的邮件）的按指纹，否则按 Message-ID + 大小。重复文件计入 duplicates，不重复入库。
- 附件随邮件写进附件缓存，是它唯一的副本（没有服务器可以再下载），所以缓存预算从 2 GiB 提到 4 GiB；解析截断用导出的 `mail.MaxAttachmentBytes` 表达，`Size` 等于上限即部分数据，导入器据此跳过落盘。

**两个入口，一个机制。** 设置 › 导入 页和 `godwit import-eml`（`-offline` 直接写关闭状态的数据目录）都跑 `importeml.Drive`：200 个文件一批，journal（数据目录旁的 `import-eml-state.json`，先写临时文件再原子替换）记下已结束的文件，中断后接着跑；失败文件不入 journal，下次重试。UI 与 CLI 不许直接进 core/mailsync、core/store（app/boundary_test.go），桥接包 `importeml/` 是它们能导入的唯一通道：两边共享同一个 journal，一边跑了一半，另一边接着跑。`Service.ImportEml` 是新的 `core.Service` 方法，进 `full`（`rpcbind/testdata/full_methods.txt`），不进 audit：它读本机文件、写本机库，无外发（[0003](0003-no-extension-can-send.md)）。

**外发。** 无。导入不发送、不上传、不通知；journal 与缓存都在数据目录里。离线模式在开库前先用数据目录锁确认应用没有在跑（Windows 独占打开 `godwit.lock`，其他平台 flock），由 `guardclosed_*.go` 分平台实现。

## Considered Options

- **IMAP APPEND 上传再同步（tools/emlimport）**：邮件回到服务器，所有设备都看得见；但慢（受上行带宽与服务器限速）、要迁就各服务器方言，且备份一旦上传就依赖服务器留不留。作为独立 helper 保留，不进产品。
- **导入为普通同步文件夹**：语义混乱——服务器上没有对应物，stale 判定和文件夹认领都会出错。
- **每个文件一次 RPC 往返**：慢；按批 200 与 journal 结合，粒度刚好，进度也按批汇报。
- **journal 放在备份目录里**：同一份备份导入两个账户会互踩；放在数据目录旁，按数据目录隔离。
- **UI 自己实现批量循环**：违反边界测试；共享 `importeml` 之后，页面只剩表单、进度和失败清单。

## Consequences

- 本地文件夹只增不减，LIST 永远救不了它：它不是服务器文件夹的镜像，删掉即真删，没有可恢复的来源。
- 导入邮件的附件被缓存驱逐后没有别的来源可取；预算提到 4 GiB 是为一次完整导入留余量，驱逐策略不变（最旧先出）。
- `mail.MaxAttachmentBytes` 从私有变为导出：解析在截断处保留前导切片，这个「部分数据」的语义由注释钉住，`core/mail/dkim_security.go` 与导入共用同一个上限。
- 本地邮件的 flags/move 不排队 ops，对服务器上有的邮件行为不变（`onServer` 才排队），既有同步语义不受影响。
- journal 与文件路径绑定：备份目录改名或移动后 journal 失配，重复导入由去重兜底，不会出现两份。
- CLI 的默认文件夹名（126邮箱备份）是维护者自己的用例，`--folder` 可改；UI 默认 Imported Mail，名字不做翻译——名字是用户要保留的数据，不是界面文案。
