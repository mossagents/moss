package appkit

import (
	"fmt"
	"strings"
)

// PrintBanner 打印统一格式的应用启动信息。
//
// 用法：
//
//	appkit.PrintBanner("mossquant", map[string]string{
//	    "Provider":  "openai",
//	    "Model":     "gpt-4o",
//	    "Workspace": ".",
//	    "Tools":     "12 loaded",
//	})
func PrintBanner(appName string, fields map[string]string) {
	// 计算框宽度：appName + 两侧各 4 字符空白 + 边框
	title := fmt.Sprintf("  %s  ", appName)
	width := len(title) + 2
	if width < 40 {
		width = 40
	}

	// 顶部边框
	fmt.Printf("╭%s╮\n", repeat("─", width))
	// 居中标题
	pad := (width - len(title)) / 2
	fmt.Printf("│%s%s%s│\n", spaces(pad), title, spaces(width-pad-len(title)))
	// 底部边框
	fmt.Printf("╰%s╯\n", repeat("─", width))

	// 有序字段输出
	for _, key := range orderedKeys(fields) {
		fmt.Printf("  %-12s %s\n", key+":", fields[key])
	}
	fmt.Println()
}

// PrintBannerWithHint 在 Banner 后打印提示信息。
func PrintBannerWithHint(appName string, fields map[string]string, hints ...string) {
	PrintBanner(appName, fields)
	for _, hint := range hints {
		fmt.Printf("  %s\n", hint)
	}
	if len(hints) > 0 {
		fmt.Println()
	}
}

func repeat(s string, n int) string {
	return strings.Repeat(s, n)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return repeat(" ", n)
}

// orderedKeys 按常见优先级排序字段名。未匹配的排在末尾。
func orderedKeys(m map[string]string) []string {
	priority := []string{"Provider", "Model", "Workspace", "Mode", "Trust", "Capital", "Workers", "Symbols", "Tools", "Goal"}
	var result []string
	seen := make(map[string]bool)

	for _, k := range priority {
		if _, ok := m[k]; ok {
			result = append(result, k)
			seen[k] = true
		}
	}
	for k := range m {
		if !seen[k] {
			result = append(result, k)
		}
	}
	return result
}
