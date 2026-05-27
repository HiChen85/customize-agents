---
title: Sandbox 实现讲解
author: customize-agents
summary: 面向初级开发者的沙箱实现说明，解释当前项目如何在 exec 工具执行前做命令、路径和输出控制。
---

# Sandbox 实现讲解

## 先说结论

当前项目里的 Sandbox 不是 Docker、VM、chroot 那种“真正隔离的运行环境”。它更准确地说是一个 **命令执行前的规则过滤器**。

也就是说：

- 它不会创建独立容器。
- 它不会限制进程的真实系统权限。
- 它不会拦截所有文件读写。
- 它主要保护的是 `exec` 工具，也就是模型想执行 shell 命令时，先检查这条命令是否符合规则。

可以把它理解成：

> 模型想执行命令之前，门口有一个保安。保安看一下命令名、路径、输出大小，符合规则才放行。

## 为什么需要它

项目里的 `exec` 工具本质上会执行：

```go
cmd := exec.CommandContext(ctx, "bash", "-c", params.Command)
output, err := cmd.CombinedOutput()
```

如果模型调用：

```json
{"command": "ls -la"}
```

程序实际运行的是：

```bash
bash -c "ls -la"
```

这个能力很强，也很危险。因为模型也可能生成：

```bash
rm -rf /
cat ~/.ssh/id_rsa
curl http://example.com/upload
```

所以 Sandbox 的目标是：在 `bash -c` 真正执行之前，先检查这条命令。

## 配置结构

Sandbox 的配置结构在 `core/sandbox.go`：

```go
type SandboxConfig struct {
    AllowedCommands []string `yaml:"allowed_commands"`
    BlockedCommands []string `yaml:"blocked_commands"`
    AllowedPaths    []string `yaml:"allowed_paths"`
    BlockedPaths    []string `yaml:"blocked_paths"`
    MaxOutputSize   int      `yaml:"max_output_size"`
}
```

这些字段的含义：

| 字段 | 含义 |
|---|---|
| `AllowedCommands` | 命令白名单。如果配置了，只有这些命令能执行 |
| `BlockedCommands` | 命令黑名单。命中就拒绝 |
| `AllowedPaths` | 路径白名单。如果配置了，命令里出现的绝对路径必须在这些目录下 |
| `BlockedPaths` | 路径黑名单。命令里出现这些路径就拒绝 |
| `MaxOutputSize` | 最大输出长度，防止命令输出太多撑爆上下文 |

`agent.yaml` 里有示例配置：

```yaml
sandbox:
  blocked_commands:
    - "rm"
    - "rmdir"
    - "curl"
    - "wget"
    - "ssh"
  allowed_commands: []
  allowed_paths:
    - "/Users/haichen.zhang/Documents"
    - "/tmp"
  blocked_paths:
    - "/etc"
    - "/root"
    - "~/.ssh"
    - "~/.aws"
  max_output_size: 102400
```

当前仓库里的示例是注释状态，所以按当前默认配置，Sandbox 没有启用。

## 什么时候启用

CLI 和 Server 入口里都有类似逻辑：

```go
if len(cfg.Sandbox.BlockedCommands) > 0 || len(cfg.Sandbox.AllowedCommands) > 0 {
    sandbox = core.NewSandbox(...)
    for i, tool := range tools {
        if tool.Definition.Name == "exec" {
            tools[i] = sandbox.WrapExecTool(tool)
            break
        }
    }
}
```

流程是：

1. 程序先创建所有工具，包括原始 `exec`。
2. 如果配置里有 `blocked_commands` 或 `allowed_commands`，就创建 Sandbox。
3. 找到名字叫 `exec` 的工具。
4. 用 `sandbox.WrapExecTool(tool)` 把它包一层。
5. 之后 Agent 调用的就是“被沙箱包装过的 exec”。

这里有一个实现细节：

> 当前启用条件只检查 `BlockedCommands` 和 `AllowedCommands`。如果只配置了路径规则，但没有配置命令规则，Sandbox 不会被启用。

## WrapExecTool：如何包住 exec

核心代码：

```go
func (s *Sandbox) WrapExecTool(tool Tool) Tool {
    originalExecute := tool.Execute

    tool.Execute = func(ctx context.Context, input json.RawMessage) (string, error) {
        var params struct {
            Command string `json:"command"`
        }

        if err := json.Unmarshal(input, &params); err != nil {
            return "", fmt.Errorf("sandbox: parse input: %w", err)
        }

        if err := s.Check(params.Command); err != nil {
            return "", err
        }

        output, err := originalExecute(ctx, input)
        if err != nil {
            return output, err
        }
        return s.TruncateOutput(output), nil
    }

    return tool
}
```

白话解释：

1. 先把原来的 `exec.Execute` 保存起来，叫 `originalExecute`。
2. 替换掉 `tool.Execute`，换成一个新的函数。
3. 新函数先解析输入里的 `command` 字段。
4. 调用 `s.Check(command)` 检查命令。
5. 检查失败：直接返回错误，不执行命令。
6. 检查通过：调用原来的 `exec`。
7. 命令执行完后，对输出做截断。

这就是典型的“装饰器模式”：不改原始 `exec` 工具，而是在外面包一层额外逻辑。

## 执行链路中的位置

当模型请求调用工具时，Agent 的执行顺序是：

```text
Permission Check
→ BeforeToolCall Hook
→ ToolExecutor
→ tool.Execute
→ AfterToolCall Hook
```

Sandbox 不在 `Agent.executeSingleTool` 里显式出现。因为它已经提前把 `exec` 工具的 `Execute` 函数替换掉了。

实际链路是：

```text
模型请求 exec
  ↓
Agent 权限检查
  ↓
BeforeToolCall Hook
  ↓
ToolExecutor 超时/重试包装
  ↓
被 Sandbox 包装过的 exec.Execute
  ↓
Sandbox.Check(command)
  ↓
真正的 exec.Execute
  ↓
Sandbox.TruncateOutput(output)
  ↓
返回 tool_result
```

换句话说：

> Sandbox 是工具自身的一层包装，不是 Agent 主循环里的一个独立步骤。

## Check：具体检查什么

核心函数：

```go
func (s *Sandbox) Check(command string) error {
    s.mu.RLock()
    cfg := s.config
    s.mu.RUnlock()

    segments := splitCommand(command)
    for _, seg := range segments {
        binary := extractBinary(seg)
        if err := s.checkBinary(binary, cfg); err != nil {
            return err
        }
        if err := s.checkPaths(seg, cfg); err != nil {
            return err
        }
    }
    return nil
}
```

它做三件事：

1. 读出当前配置。
2. 把命令拆成多个片段。
3. 对每个片段检查命令名和路径。

例如：

```bash
ls && rm -rf /
```

会拆成：

```text
ls
rm -rf /
```

然后分别检查。只要其中一个片段不合法，整条命令就拒绝。

## 第一步：拆命令

`splitCommand` 会按这些 shell 分隔符拆：

```text
&&
||
;
```

示例：

```bash
ls && rm -rf /
```

拆成：

```text
ls
rm -rf /
```

```bash
cat foo.txt; curl http://example.com
```

拆成：

```text
cat foo.txt
curl http://example.com
```

当前实现不会按管道 `|` 拆命令，所以：

```bash
echo hello | grep h
```

会被当成一个片段处理。测试里也明确这个例子是允许的。

这说明当前实现不是完整 shell parser，而是一个简单规则检查器。

## 第二步：提取命令名

`extractBinary` 的逻辑：

```go
func extractBinary(segment string) string {
    segment = strings.TrimSpace(segment)
    fields := strings.Fields(segment)
    if len(fields) == 0 {
        return ""
    }

    binary := fields[0]
    if idx := strings.LastIndex(binary, "/"); idx >= 0 {
        binary = binary[idx+1:]
    }
    return binary
}
```

它会：

1. 去掉前后空格。
2. 用空格切分。
3. 取第一个词。
4. 如果第一个词是路径，就只取最后的文件名。

例子：

```bash
rm -rf /tmp/foo
```

命令名是：

```text
rm
```

```bash
/usr/bin/git status
```

命令名是：

```text
git
```

所以配置里写 `git` 就能匹配 `/usr/bin/git`。

## 第三步：检查命令黑名单

逻辑：

```go
for _, blocked := range cfg.BlockedCommands {
    if binary == blocked {
        return fmt.Errorf("sandbox: command '%s' is blocked", binary)
    }
}
```

如果配置：

```yaml
blocked_commands:
  - rm
  - curl
  - wget
```

那么这些命令都会被拒绝：

```bash
rm -rf /tmp/foo
curl http://example.com
wget http://example.com/file
```

即使危险命令出现在后半段，也会被发现：

```bash
ls && rm -rf /
```

因为它会先拆成 `ls` 和 `rm -rf /`。

## 第四步：检查命令白名单

逻辑：

```go
if len(cfg.AllowedCommands) > 0 {
    allowed := false
    for _, a := range cfg.AllowedCommands {
        if binary == a {
            allowed = true
            break
        }
    }
    if !allowed {
        return fmt.Errorf("sandbox: command '%s' is not in allowed list", binary)
    }
}
```

白名单的含义：

> 如果配置了 `AllowedCommands`，那么只有列表里的命令能执行。

例如：

```yaml
allowed_commands:
  - ls
  - cat
  - grep
```

这些允许：

```bash
ls -la
cat file.txt
grep foo bar.txt
```

这些拒绝：

```bash
python script.py
rm -rf /
git status
```

黑名单优先于白名单。如果一个命令既在黑名单又在白名单里，仍然会被拒绝。

## 第五步：检查路径

路径检查逻辑：

```go
tokens := strings.Fields(segment)
for _, token := range tokens {
    if !strings.HasPrefix(token, "/") && !strings.HasPrefix(token, "~/") {
        continue
    }

    for _, blocked := range cfg.BlockedPaths {
        if strings.HasPrefix(token, blocked) {
            return fmt.Errorf("sandbox: path '%s' is blocked", token)
        }
    }

    if len(cfg.AllowedPaths) > 0 {
        ...
    }
}
```

它会把命令片段按空格拆成 token，然后只关心两类 token：

```text
以 / 开头的绝对路径
以 ~/ 开头的用户目录路径
```

例如：

```bash
cat /etc/passwd
```

token 包括：

```text
cat
/etc/passwd
```

`/etc/passwd` 会被拿去检查路径规则。

如果配置：

```yaml
blocked_paths:
  - /etc
  - ~/.ssh
```

那么这些会被拒绝：

```bash
cat /etc/passwd
ls ~/.ssh/
```

如果配置：

```yaml
allowed_paths:
  - /tmp
  - /Users/haichen.zhang/Documents
```

那么这些允许：

```bash
cat /tmp/a.txt
ls /Users/haichen.zhang/Documents/code
```

这些拒绝：

```bash
cat /etc/passwd
ls /var/log
```

注意：它只检查命令字符串里直接出现的路径 token，不是文件系统级拦截。

## 第六步：限制输出大小

命令执行成功后，Sandbox 会调用：

```go
func (s *Sandbox) TruncateOutput(output string) string {
    if maxSize <= 0 || len(output) <= maxSize {
        return output
    }
    return output[:maxSize] + fmt.Sprintf("\n... [truncated, total %d bytes]", len(output))
}
```

默认最大输出是 102400 字节，也就是约 100KB：

```go
if config.MaxOutputSize <= 0 {
    config.MaxOutputSize = 102400
}
```

为什么要截断？

因为 Agent 会把工具输出塞回对话上下文。如果命令输出特别大，比如：

```bash
cat huge.log
find / -type f
```

可能会把上下文撑爆，也会让后续 LLM 请求变慢。输出截断是为了保护上下文窗口。

## 第七步：支持热更新

Sandbox 内部保存配置时用了锁：

```go
type Sandbox struct {
    config SandboxConfig
    mu     sync.RWMutex
}
```

读配置时用读锁：

```go
s.mu.RLock()
cfg := s.config
s.mu.RUnlock()
```

更新配置时用写锁：

```go
func (s *Sandbox) UpdateConfig(config SandboxConfig) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if config.MaxOutputSize <= 0 {
        config.MaxOutputSize = 102400
    }
    s.config = config
}
```

Server 模式里，配置文件变化时会调用：

```go
cfgWatcher.OnReload(func(oldCfg, newCfg *config.Config) {
    if sandbox != nil {
        sandbox.UpdateConfig(...)
        slog.Info("sandbox config reloaded")
    }
})
```

也就是说，服务运行中改配置文件，Sandbox 可以更新规则，不需要重启进程。

但有个前提：

> 只有启动时已经创建了 `sandbox`，热更新才会生效。如果启动时没有启用 sandbox，后续只改配置文件加规则，目前不会自动新建一个 sandbox。

## 和 Permission 的区别

Permission 判断的是：

> 这个工具能不能被调用？

例如：

```text
exec 这个工具要不要允许？
write_file 这个工具要不要允许？
read_file 是否自动放行？
```

Sandbox 判断的是：

> 如果已经允许调用 exec，那么 exec 里面这条具体 shell 命令是否安全？

完整关系：

```text
模型想调用 exec
  ↓
Permission：是否允许调用 exec？
  ↓
Sandbox：允许调用 exec 后，里面的 command 是不是 rm/curl/越界路径？
```

所以它们是两层不同的保护。

## 一个完整例子

假设配置是：

```yaml
sandbox:
  blocked_commands:
    - rm
    - curl
  blocked_paths:
    - /etc
    - ~/.ssh
  max_output_size: 102400
```

模型请求：

```json
{
  "command": "ls && cat /etc/passwd"
}
```

执行过程：

1. Agent 收到 `exec` 工具调用。
2. Permission 先判断是否允许调用 `exec`。
3. 进入 ToolExecutor。
4. 调用被 Sandbox 包装过的 `exec.Execute`。
5. Sandbox 解析出 `command`。
6. `splitCommand` 拆成 `ls` 和 `cat /etc/passwd`。
7. 第一段 `ls` 检查通过。
8. 第二段 `cat /etc/passwd` 的命令名 `cat` 不在黑名单。
9. 路径 `/etc/passwd` 以 `/etc` 开头，命中 `BlockedPaths`。
10. Sandbox 返回错误。
11. 原始 `exec` 不会执行。
12. LLM 收到一个错误形式的 `tool_result`。

## 当前实现的优点

| 优点 | 说明 |
|---|---|
| 简单直接 | 规则集中在 `core/sandbox.go`，容易阅读和测试 |
| 轻量 | 不依赖 Docker、VM 或系统特性 |
| 可配置 | 通过 YAML 控制命令、路径和输出大小 |
| 可热更新 | Server 模式下可运行时更新配置 |
| 影响面小 | 只包装高风险的 `exec` 工具 |
| 可组合 | 可以和 Permission、Hook、ToolExecutor 一起工作 |

## 当前实现的局限

### 1. 它不是系统级隔离

它没有使用：

- Linux namespace
- seccomp
- chroot
- container
- VM
- macOS sandbox profile

所以一旦某条命令通过检查，它实际还是以当前进程用户权限执行。

### 2. 它只包装 `exec`

它不会保护：

- `read_file`
- `write_file`
- `web_fetch`
- `web_search`
- MCP 工具
- 其他未来新增工具

如果 `write_file` 写敏感路径，目前不是 Sandbox 负责的，需要靠 Permission 或工具自身逻辑控制。

### 3. 命令解析比较朴素

它只按这些符号拆：

```text
&&
||
;
```

不完整处理：

```text
|
$(...)
`...`
>
>>
<
环境变量展开
通配符 *
引号里的分隔符
子 shell
复杂 shell 语法
```

所以它不是完整的 shell 解析器。

### 4. 路径检查只看字符串 token

它只看命令里显式出现的：

```text
/path
~/path
```

不理解变量、软链接、命令替换等复杂情况。例如：

```bash
cat $HOME/.ssh/id_rsa
```

这个 token 不以 `/` 或 `~/` 开头，当前路径检查不会处理。

### 5. 不检查 `work_dir`

`exec` 工具有 `work_dir` 参数：

```go
WorkDir string `json:"work_dir"`
```

但 Sandbox 当前只解析：

```go
Command string `json:"command"`
```

也就是说，它检查的是 `command`，没有检查 `work_dir` 是否落在允许路径里。

### 6. 启用条件不包括路径规则

CLI 和 Server 只有在 `BlockedCommands` 或 `AllowedCommands` 非空时才创建 Sandbox。

如果只配置：

```yaml
sandbox:
  blocked_paths:
    - /etc
```

当前不会启用 Sandbox。

## 怎么记住这个设计

可以把它想成三层门：

```text
第一层：Permission
判断这个工具能不能用。

第二层：Sandbox
如果用的是 exec，判断这条 shell 命令能不能跑。

第三层：ToolExecutor
如果允许执行，给它加超时和重试保护。
```

而 Sandbox 自己内部又是四步：

```text
1. 拆命令
2. 查命令黑名单/白名单
3. 查路径黑名单/白名单
4. 截断输出
```

## 后续加强方向

如果要把这个 Sandbox 做得更可靠，可以优先考虑：

1. 启用条件改成：只要 commands、paths 或 `max_output_size` 任一 sandbox 配置存在，就启用。
2. 检查 `exec` 的 `work_dir`。
3. 使用更可靠的 shell 解析器，至少处理 `|`、重定向和子命令。
4. 对 `read_file` 和 `write_file` 也增加路径策略。
5. 把路径规则从字符串前缀升级为 `filepath.Clean`、绝对路径解析和 symlink 检查。
6. 对生产场景，考虑真正的 OS 级隔离，比如容器、受限用户、seccomp、macOS sandbox-exec 等。

当前实现适合作为“轻量安全护栏”，但不要把它当成“恶意命令绝对无法逃逸”的强安全边界。
