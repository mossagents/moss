# custom-tool — 自定义工具注册示例

演示如何在 moss kernel 中注册自定义工具，与内置工具共存。

## 包含的自定义工具

| 工具 | 描述 | 风险等级 |
|---|---|---|
| `calculator` | 四则运算 (+, -, *, /) | Low |
| `random_number` | 生成指定范围随机整数 | Low |

## 用法

```bash
go run . --provider openai --model gpt-4o
```

对话示例：
```
you> 帮我算 123 乘以 456
🔧 Running calculator...
✅ {"expression": "123 * 456", "result": 56088}
123 × 456 = 56088

you> 给我一个 1 到 100 的随机数
🔧 Running random_number...
✅ {"result": 42}
随机数是 42。
```

## 如何注册自定义工具

```go
// 1. 定义工具元信息
spec := tool.ToolSpec{
    Name:        "my_tool",
    Description: "What this tool does",
    InputSchema: json.RawMessage(`{...}`), // JSON Schema
    Risk:        tool.RiskLow,
}

// 2. 实现处理函数
handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
    // 解析输入、执行逻辑、返回结果
    return json.Marshal(result)
}

// 3. 注册到 kernel
k.ToolRegistry().Register(spec, handler)
```

风险等级：`RiskLow`（只读）、`RiskMedium`（有限副作用）、`RiskHigh`（文件写入/命令执行）。
高风险工具在 `restricted` 模式下会触发用户确认。
