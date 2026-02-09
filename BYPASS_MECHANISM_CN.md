# Lumine 绕过审查机制详解

## 概述

Lumine 是一个审查规避工具，使用多种流量操纵技术来绕过深度包检测（DPI）系统。本文档详细解释这些机制的工作原理。

## 审查系统的工作方式

网络审查系统通常使用深度包检测（DPI）来：
1. **识别 TLS 连接** - 通过检查 ClientHello 握手
2. **提取服务器名称指示（SNI）字段** - 确定访问目标
3. **阻断或重置连接** - 对被禁域名进行拦截

## 绕过技术详解

### 1. TLS 记录分片（tls-rf）

这是你日志中显示的主要绕过机制。

#### 工作原理

**正常的 TLS 握手：**
```
客户端 → [完整的 ClientHello 数据包] → 服务器
         ↑ DPI 可以轻松检查 SNI
```

**使用 TLS-RF 后：**
```
客户端 → [片段1][片段2][片段3][片段4] → 服务器
         ↑ SNI 被分散到多个 TLS 记录中
```

#### 实现细节

`tls-rf` 模式执行以下步骤：

1. **拦截 ClientHello**：在发送前捕获 TLS ClientHello 数据包
2. **分割成多个记录**：将 ClientHello 分割成多个 TLS 记录
3. **策略性分片**：特别在 SNI 字段周围进行分割，使域名被分散
4. **可选修改**：
   - `mod_minor_ver`：修改记录头中的 TLS 次版本号（例如从 0x03 改为 0x04）
   - `oob`：在第一个段附加带外数据
   - `num_records`：控制创建多少个 TLS 记录（如你日志中的 4 个记录）
   - `num_segs`：控制 TCP 层的分段

#### 代码位置
- 主要逻辑：`fragment.go` - `sendRecords()` 函数
- 记录分割：`splitAndAppend()` 函数

#### 为什么有效

DPI 系统通常有以下限制：
- **无状态检测**：许多 DPI 系统不会重组分片的 TLS 记录
- **性能限制**：重组需要缓冲和处理能力
- **协议异常**：不寻常的次版本修改可能会混淆解析器

### 2. 基于 TTL 的去同步（ttl-d）

这种技术利用 IP 数据包中的生存时间（TTL）字段。

#### 工作原理

**步骤 1：TTL 探测**
```go
// minReachableTTL() 在 desync_linux.go 中
// 二分查找找到能到达服务器的最小 TTL
low, high := 1, maxTTL
for low <= high {
    mid := (low + high) / 2
    // 尝试用 TTL = mid 连接
    // 如果成功，服务器在此 TTL 下可达
}
```

**步骤 2：发送假数据包**
```
客户端 → [假 ClientHello, TTL=5] → DPI 系统 → [被丢弃]
                                    ↓
                                 服务器（不可达，TTL 过期）
```

假数据包包含无效或截断的 SNI，会被 DPI 检查但不会到达服务器。

**步骤 3：等待并重置 TTL**
```go
time.Sleep(fakeSleep) // 等待 DPI 处理
```

**步骤 4：发送真实数据包**
```
客户端 → [真实 ClientHello, TTL=64] → DPI 系统 → 服务器
                                      ↑ 已处理假数据包
                                      ↓ 忽略真实数据包
```

#### 实现细节

在 Linux 上（`desync_linux.go`）：
- 使用 `vmsplice()` 和 `splice()` 进行零拷贝数据包操作
- 使用内存映射页面实现高效数据处理
- 通过 `IP_TTL` socket 选项精确控制 TTL

在 Windows 上（`desync_windows.go`）：
- 使用 `TransmitFile()` API 进行异步数据包传输
- 基于临时文件的数据暂存方法
- 使用 `WSASend()` 进行网络操作

#### 为什么有效

1. **DPI 状态混淆**：DPI 系统处理假数据包并可能创建连接状态
2. **时间窗口**：处理假数据包后，存在一个真实数据包可以通过的窗口
3. **TTL 精度**：通过将假数据包的 TTL 设置为刚好低于到达服务器的阈值，保证它在到达目的地前被丢弃

### 3. 次版本号修改

当启用 `mod_minor_ver` 时：

```go
// 在 fragment.go 的 sendRecords() 中
if modMinorVer {
    header[2] = 0x04  // 修改次版本号
}
```

标准 TLS 1.2 记录头：`0x16 0x03 0x03`
修改后的头：`0x16 0x03 0x04`

这个微妙的改变：
- 技术上是有效的（服务器通常忽略记录中的次版本号）
- 可能混淆期望精确字节序列的 DPI 模式匹配
- 保持协议兼容性

### 4. 带外（OOB）数据

在第一个段发送 OOB 数据（TCP 紧急数据）：

```go
// Linux：desync_linux.go
unix.Sendto(int(fd), []byte{'&'}, unix.MSG_OOB, nil)

// Windows：desync_windows.go  
windows.WSASend(sock, &wsabuf, 1, &bytesSent, windows.MSG_OOB, nil, nil)
```

这利用了：
- 一些 DPI 系统不能正确处理 TCP 紧急指针
- OOB 数据可以混淆数据包检测逻辑
- 在数据包边界创建歧义

## 日志分析

基于你提供的日志：

```
[H00002] 2026/02/09 06:09:52 Policy: 142.251.119.90 | tls-rf | 4 records | mod_minor_ver
```

这显示：
- **目标 IP**：142.251.119.90（Google/YouTube）
- **模式**：tls-rf（TLS 记录分片）
- **配置**：4 个记录，启用次版本修改
- **结果**："Successfully sent ClientHello" - 绕过成功

```
[S00256] 2026/02/09 06:09:52 Redirect 172.64.229.211 to www.ietf.org
```

这显示 IP 重定向：
- 配置将某些 IP 映射到不同的主机
- 用于绕过基于 IP 的阻断
- 保持 DNS 解析的灵活性

```
[H00001] 2026/02/09 06:09:53 DNS: static.doubleclick.net -> 142.250.69.166
[H00001] 2026/02/09 06:09:53 Policy: timeout=10s | tls-rf | 4 records | mod_minor_ver
```

这显示：
- **DNS 解析**：域名被解析为 IP 地址
- **策略应用**：应用了带超时的 tls-rf 策略
- **分片配置**：使用 4 个记录进行 TLS 分片

## 配置示例

### TLS-RF 模式（来自你的日志）
```json
{
  "domain_policy": {
    "*.youtube.com": {
      "mode": "tls-rf",
      "num_records": 4,
      "mod_minor_ver": true
    }
  }
}
```

### TTL-D 模式自动检测
```json
{
  "domain_policy": {
    "*.blocked-site.com": {
      "mode": "ttl-d",
      "fake_ttl": 0,
      "fake_sleep": "200ms",
      "attempts": 3,
      "max_ttl": 30
    }
  }
}
```

## 对抗 DPI 的原理

这些技术之所以有效，是因为它们利用了 DPI 系统的基本限制：

1. **计算限制**：完整的重组和状态跟踪成本高昂
2. **时序限制**：DPI 必须快速做出决策以避免延迟
3. **协议复杂性**：TLS 和 TCP 有许多边缘情况
4. **向后兼容性**：服务器必须接受略微畸形但有效的数据

## 技术细节总结

### TLS-RF 模式的关键点

1. **分片位置**：在 SNI 字段周围进行分割
   ```go
   // 在 fragment.go 中
   cut := offset - 5 + 1 + 2  // 计算分割点
   ```

2. **记录重建**：
   - 左侧块：从开始到 SNI 之前
   - 右侧块：从 SNI 到结束
   - 每个块进一步分割成指定数量的记录

3. **TCP 分段**（可选）：
   - `num_segs = 1`：一次发送所有记录
   - `num_segs > 1`：将合并的记录分成多个 TCP 段
   - `num_segs = -1`：每次发送一个记录

### TTL-D 模式的关键点

1. **TTL 探测算法**：
   - 使用二分查找找到最小可达 TTL
   - 缓存结果以提高性能
   - 支持 IPv4 和 IPv6

2. **假数据包构造**：
   ```go
   // 在 desync_linux.go 中
   // 方法 1：在最后一个点之前截断
   cut := sniPos + sniLen  // 找到 SNI 中的最后一个 '.'
   
   // 方法 2：如果没有点，在中间截断
   cut = sniLen/2 + sniPos
   ```

3. **零拷贝优化**（Linux）：
   - `mmap()`：分配内存映射页面
   - `vmsplice()`：将内存页面移动到管道
   - `splice()`：从管道传输到 socket

## 局限性

这些技术可能无法对抗：
- **复杂的 DPI**：执行完整重组的系统
- **应用层代理**：中间人 TLS 检测
- **基于 IP 的阻断**：如果目标 IP 本身被阻断
- **协议指纹识别**：检测操纵模式本身

## 为什么这对绕过审查很重要

1. **分散检测点**：通过分片，SNI 字段不在单一数据包中完整出现
2. **混淆 DPI 逻辑**：次版本修改和 OOB 数据创建异常模式
3. **利用时间窗口**：TTL 技术利用 DPI 处理的时间差
4. **保持合法性**：所有技术都使用合法的协议功能，服务器端完全兼容

## 实际应用场景

从你的日志可以看出，Lumine 成功地：

1. **绕过 YouTube 审查**：
   - 使用 tls-rf 模式
   - 4 个记录分片
   - 次版本修改

2. **处理 DNS 重定向**：
   - 将被阻断的 IP 重定向到可访问的主机
   - 保持连接的灵活性

3. **支持多种协议**：
   - SOCKS5 代理（端口 1080）
   - HTTP 代理（端口 1225）

## 参考资料

- 代码：`fragment.go`、`desync_linux.go`、`desync_windows.go`、`utils.go`
- 基于：[TlsFragment](https://github.com/maoist2009/TlsFragment)
- 相关项目：Geneva（遗传规避）、GoodbyeDPI、PowerTunnel

## 总结

Lumine 的绕过机制通过以下方式工作：

1. **TLS-RF**：将 TLS ClientHello 分片，使 DPI 无法检测完整的 SNI
2. **TTL-D**：发送会在 DPI 处被检测但不会到达服务器的假数据包
3. **协议修改**：微小的协议变化混淆 DPI 模式匹配
4. **组合使用**：多种技术可以组合使用以提高绕过成功率

这些技术都是在客户端实现的，不需要服务器端的任何修改，因为所有的操纵都在协议规范允许的范围内。
