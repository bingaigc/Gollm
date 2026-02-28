# 🚀 Gollm - Go 语言顶级工程师成长之路

> **30天硬核实战训练营，涵盖云原生×AI Agent方向**
>
> 从零基础到能独立开发云原生微服务 & AI Agent 的 Go 工程师，每一天都有明确目标、实战代码和配套文档。

[![Go Version](https://img.shields.io/badge/Go-1.24-blue?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## 📑 目录

- [项目简介](#-项目简介)
- [仓库结构](#-仓库结构)
- [六大战区总览](#-六大战区总览)
- [30天训练营概览](#-30天训练营概览)
- [快速开始](#-快速开始)
- [最终能力清单](#-最终能力清单)
- [学习方法论](#-学习方法论)
- [许可证](#-许可证)

---

## 🎯 项目简介

**Gollm** 是一个面向有志成为 **Go 语言顶级工程师** 的系统化学习仓库。训练营以 **30 天** 为周期，采用 **"战区制"** 编排，覆盖从语言基础、系统编程、云原生微服务、Kubernetes 生态，到 AI 基建与 Agent 开发等全栈核心技能。

✨ **特色亮点**

| 特色 | 说明 |
|------|------|
| 🗓️ 30 天结构化路径 | 每天一个明确主题，循序渐进 |
| 🔥 六大战区覆盖 | 语言基础 → 系统核心 → 云原生 → K8s → AI Agent → 高级架构 |
| 💻 全实战驱动 | 每天配套可运行示例代码 |
| 📖 配套文档 | `docs/` 目录提供深度指南与提示词参考 |
| 🤖 AI 原生方向 | 深度整合 LLM / MCP / AI Agent 开发实践 |

---

## 📁 仓库结构

```
Gollm/
├── 📄 go.mod                          # Go 模块定义
├── 📄 README.md                       # 本文件
│
├── 📂 docs/                           # 文档资料
│   ├── 📂 guide/                      # 深度学习指南
│   └── 📂 prompts/                    # AI 提示词模板
│
└── 📂 examples/                       # 实战示例代码
    ├── 📂 week1/                      # 第一周：语言入门 & 系统核心
    │   ├── 📂 day01-cli/              # Day01 - CLI 工具开发
    │   ├── 📂 day02-slices/           # Day02 - 切片与集合
    │   ├── 📂 day03-interfaces/       # Day03 - 接口与多态
    │   ├── 📂 day04-errors/           # Day04 - 错误处理
    │   ├── 📂 day05-goroutines/       # Day05 - Goroutine 并发
    │   ├── 📂 day06-channels/         # Day06 - Channel 通信
    │   └── 📂 day07-context/          # Day07 - Context 控制
    │
    ├── 📂 week2/                      # 第二周：云原生微服务
    │   ├── 📂 day08-protobuf/         # Day08 - Protobuf 序列化
    │   ├── 📂 day09-protobuf-advanced/# Day09 - Protobuf 进阶
    │   ├── 📂 day10-grpc-server/      # Day10 - gRPC 服务端
    │   ├── 📂 day11-grpc-client/      # Day11 - gRPC 客户端
    │   ├── 📂 day12-interceptors/     # Day12 - 拦截器与中间件
    │   ├── 📂 day13-middleware/        # Day13 - 高级中间件模式
    │   └── 📂 day14-pprof/            # Day14 - pprof 性能分析
    │
    ├── 📂 week3/                      # 第三周：AI 基建与 Agent
    │   ├── 📂 day15-llm-client/       # Day15 - LLM 客户端
    │   ├── 📂 day16-llm-advanced/     # Day16 - LLM 高级调用
    │   ├── 📂 day17-sse/              # Day17 - SSE 流式传输
    │   ├── 📂 day18-sse-advanced/     # Day18 - SSE 进阶模式
    │   ├── 📂 day19-mcp-server/       # Day19 - MCP Server 开发
    │   ├── 📂 day20-mcp-advanced/     # Day20 - MCP 协议进阶
    │   └── 📂 day21-ai-agent/         # Day21 - AI Agent 构建
    │
    ├── 📂 week4/                      # 第四周：K8s 生态编程
    │   ├── 📂 day22-k8s-basics/       # Day22 - K8s 基础概念
    │   ├── 📂 day24-k8s-deploy/       # Day24 - K8s 部署
    │   ├── 📂 day26-client-go/        # Day26 - client-go 编程
    │   └── 📂 day28-observability/    # Day28 - 可观测性
    │
    └── 📂 week5/                      # 第五周：高级架构与底层
        └── 📂 day29-benchmark/        # Day29 - 基准测试与调优
```

---

## ⚔️ 六大战区总览

### 🟢 战区零：语言入门

> **目标**：掌握 Go 语言基础语法、工具链和开发环境搭建

| 主题 | 内容要点 |
|------|---------|
| 环境搭建 | Go 安装、GOPATH vs Go Modules、IDE 配置 |
| 基础语法 | 变量、常量、控制流、函数 |
| CLI 工具 | 使用标准库构建命令行工具 |
| 数据结构 | 切片、映射、结构体 |

---

### 🔵 战区一：系统语言核心

> **目标**：精通 Go 并发模型、接口设计和错误处理等核心机制

| 主题 | 内容要点 |
|------|---------|
| 接口与多态 | interface 设计模式、类型断言、组合优于继承 |
| 错误处理 | error wrapping、自定义错误、panic/recover |
| Goroutine | 轻量级线程、调度原理、并发模式 |
| Channel | 有缓冲/无缓冲、select 多路复用、扇入扇出 |
| Context | 超时控制、取消传播、值传递 |

---

### 🟡 战区二：云原生微服务

> **目标**：能独立设计和开发生产级 gRPC 微服务

| 主题 | 内容要点 |
|------|---------|
| Protobuf | .proto 文件定义、代码生成、序列化性能 |
| gRPC | 四种通信模式、服务端/客户端开发 |
| 拦截器 | 日志、认证、限流中间件 |
| 性能调优 | pprof 火焰图、内存/CPU 分析 |

---

### 🟠 战区三：K8s 生态编程

> **目标**：具备 Kubernetes 原生应用开发和运维编程能力

| 主题 | 内容要点 |
|------|---------|
| K8s 部署 | Dockerfile 编写、Deployment/Service YAML |
| client-go | Informer、Lister、动态客户端 |
| 可观测性 | Prometheus 指标、分布式追踪、结构化日志 |

---

### 🔴 战区四：AI 基建与 Agent 开发

> **目标**：能用 Go 构建 LLM 应用、MCP 服务和自主 AI Agent

| 主题 | 内容要点 |
|------|---------|
| LLM 客户端 | OpenAI/兼容 API 调用、流式响应处理 |
| SSE 传输 | Server-Sent Events 实现实时推送 |
| MCP Server | Model Context Protocol 服务端开发 |
| AI Agent | 工具调用、记忆管理、多步推理链 |

---

### 🟣 战区五：高级架构与底层

> **目标**：掌握性能优化、底层原理和架构设计

| 主题 | 内容要点 |
|------|---------|
| 基准测试 | benchmark 编写、性能对比、调优策略 |
| 内存管理 | GC 原理、逃逸分析、sync.Pool |
| 架构设计 | 整洁架构、DDD、CQRS 模式 |

---

## 📅 30天训练营概览

### 🗓️ 第一周：Go 语言基础 & 系统核心

> 战区零 + 战区一 | 从零到精通核心语言特性

| 天数 | 主题 | 战区 | 示例目录 |
|:----:|------|:----:|---------|
| Day 01 | 🛠️ CLI 工具开发 | 零 | `examples/week1/day01-cli/` |
| Day 02 | 📦 切片与集合操作 | 零 | `examples/week1/day02-slices/` |
| Day 03 | 🔌 接口与多态设计 | 一 | `examples/week1/day03-interfaces/` |
| Day 04 | ⚠️ 错误处理范式 | 一 | `examples/week1/day04-errors/` |
| Day 05 | ⚡ Goroutine 并发编程 | 一 | `examples/week1/day05-goroutines/` |
| Day 06 | 📡 Channel 通信模式 | 一 | `examples/week1/day06-channels/` |
| Day 07 | 🎛️ Context 超时与取消 | 一 | `examples/week1/day07-context/` |

---

### 🗓️ 第二周：云原生微服务实战

> 战区二 | 掌握 gRPC + Protobuf + 性能调优

| 天数 | 主题 | 战区 | 示例目录 |
|:----:|------|:----:|---------|
| Day 08 | 📋 Protobuf 协议设计 | 二 | `examples/week2/day08-protobuf/` |
| Day 09 | 📋 Protobuf 进阶实践 | 二 | — |
| Day 10 | 🖥️ gRPC Server 开发 | 二 | `examples/week2/day10-grpc-server/` |
| Day 11 | 📱 gRPC Client 开发 | 二 | — |
| Day 12 | 🔗 拦截器与中间件 | 二 | `examples/week2/day12-interceptors/` |
| Day 13 | 🔗 高级中间件模式 | 二 | — |
| Day 14 | 🔥 pprof 性能剖析 | 二 | `examples/week2/day14-pprof/` |

---

### 🗓️ 第三周：AI 基建与 Agent 开发

> 战区四 | 拥抱 LLM / SSE / MCP / AI Agent

| 天数 | 主题 | 战区 | 示例目录 |
|:----:|------|:----:|---------|
| Day 15 | 🤖 LLM 客户端开发 | 四 | `examples/week3/day15-llm-client/` |
| Day 16 | 🤖 LLM 高级调用 | 四 | — |
| Day 17 | 📡 SSE 流式推送 | 四 | `examples/week3/day17-sse/` |
| Day 18 | 📡 SSE 进阶模式 | 四 | — |
| Day 19 | 🧩 MCP Server 构建 | 四 | `examples/week3/day19-mcp-server/` |
| Day 20 | 🧩 MCP 协议进阶 | 四 | — |
| Day 21 | 🦾 AI Agent 实战 | 四 | `examples/week3/day21-ai-agent/` |

---

### 🗓️ 第四周：K8s 生态编程

> 战区三 | Kubernetes 原生应用开发

| 天数 | 主题 | 战区 | 示例目录 |
|:----:|------|:----:|---------|
| Day 22 | ☸️ K8s 基础概念 | 三 | — |
| Day 23 | ☸️ 容器化实践 | 三 | — |
| Day 24 | 🚀 K8s 部署实战 | 三 | `examples/week4/day24-k8s-deploy/` |
| Day 25 | 🔧 client-go 入门 | 三 | — |
| Day 26 | 🔧 client-go 实战 | 三 | `examples/week4/day26-client-go/` |
| Day 27 | 📊 可观测性入门 | 三 | — |
| Day 28 | 📊 可观测性实战 | 三 | `examples/week4/day28-observability/` |

---

### 🗓️ 第五周：高级架构与收官

> 战区五 | 性能优化、架构设计与总结

| 天数 | 主题 | 战区 | 示例目录 |
|:----:|------|:----:|---------|
| Day 29 | ⚡ 基准测试与调优 | 五 | `examples/week5/day29-benchmark/` |
| Day 30 | 🏆 综合项目与总结 | 五 | — |

---

## 🚀 快速开始

### 环境要求

- **Go** >= 1.24（[下载安装](https://go.dev/dl/)）
- **Git**
- 推荐 IDE：VS Code + Go 扩展 / GoLand

### 克隆仓库

```bash
git clone https://github.com/bingaigc/Gollm.git
cd Gollm
```

### 安装依赖

```bash
go mod tidy
```

### 运行示例

```bash
# 运行指定天数的示例（以 Day01 为例）
cd examples/week1/day01-cli
go run main.go

# 运行测试
go test ./...
```

### 阅读文档

```bash
# 学习指南
ls docs/guide/

# AI 提示词参考
ls docs/prompts/
```

---

## ✅ 最终能力清单

完成 30 天训练营后，你将掌握以下核心能力：

| 序号 | 能力项 | 战区 | 等级 |
|:----:|--------|:----:|:----:|
| 1 | Go 语言基础语法与工具链 | 零 | ⭐⭐⭐ |
| 2 | 切片、映射、结构体高级用法 | 零 | ⭐⭐⭐ |
| 3 | 接口设计与组合模式 | 一 | ⭐⭐⭐⭐ |
| 4 | 错误处理与防御式编程 | 一 | ⭐⭐⭐⭐ |
| 5 | Goroutine + Channel 并发编程 | 一 | ⭐⭐⭐⭐⭐ |
| 6 | Context 超时/取消/链式传播 | 一 | ⭐⭐⭐⭐ |
| 7 | Protobuf 协议设计与代码生成 | 二 | ⭐⭐⭐⭐ |
| 8 | gRPC 四种通信模式开发 | 二 | ⭐⭐⭐⭐⭐ |
| 9 | 拦截器/中间件设计 | 二 | ⭐⭐⭐⭐ |
| 10 | pprof 性能剖析与调优 | 二 | ⭐⭐⭐⭐ |
| 11 | LLM API 集成与流式处理 | 四 | ⭐⭐⭐⭐⭐ |
| 12 | SSE 实时推送服务 | 四 | ⭐⭐⭐⭐ |
| 13 | MCP Server 开发 | 四 | ⭐⭐⭐⭐⭐ |
| 14 | AI Agent 工具调用与推理链 | 四 | ⭐⭐⭐⭐⭐ |
| 15 | Kubernetes 容器化部署 | 三 | ⭐⭐⭐⭐ |
| 16 | client-go 编程 | 三 | ⭐⭐⭐⭐ |
| 17 | 可观测性体系（指标/追踪/日志） | 三 | ⭐⭐⭐⭐ |
| 18 | 基准测试与性能调优 | 五 | ⭐⭐⭐⭐ |

---

## 💡 学习方法论

### 🎯 每日学习节奏

```
📖 阅读文档 (30min)  →  💻 编写代码 (60min)  →  🧪 测试验证 (30min)  →  📝 总结复盘 (15min)
```

### 📌 高效学习建议

1. **先跑通再理解** — 先把示例代码跑起来，观察输出，再深入理解原理
2. **动手改代码** — 在示例基础上做修改和扩展，加深理解
3. **写测试** — 为每个练习编写单元测试，培养测试驱动开发习惯
4. **做笔记** — 记录每天的关键收获和遇到的坑
5. **及时复盘** — 每周末回顾本周内容，查漏补缺

### 🔄 战区推进策略

```
战区零 (语言入门)
  ↓
战区一 (系统核心)        ← 打好基础，不要急于求成
  ↓
战区二 (云原生微服务)    ← 开始接触生产级开发
  ↓
战区四 (AI Agent)        ← 紧跟前沿方向
  ↓
战区三 (K8s 生态)        ← 掌握部署与运维
  ↓
战区五 (高级架构)        ← 融会贯通，形成体系
```

### ⚠️ 常见误区

| 误区 | 正确做法 |
|------|---------|
| ❌ 只看不写 | ✅ 每天至少写 50 行代码 |
| ❌ 跳过基础直接上框架 | ✅ 战区零和战区一是根基 |
| ❌ 复制粘贴不思考 | ✅ 手动敲代码，理解每一行 |
| ❌ 遇到问题就放弃 | ✅ 调试是最好的学习方式 |

---

## 📄 许可证

本项目基于 [MIT License](LICENSE) 开源。

欢迎 Star ⭐、Fork 🍴 和提交 PR，一起成长！

---

<p align="center">
  <b>🔥 30 天后，你将不再是同一个 Go 开发者 🔥</b>
</p>
