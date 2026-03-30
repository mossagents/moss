package tools

import "github.com/mossagents/moss/kernel/tool"

// RegisterJinaTools 注册 Jina 搜索与阅读工具到 registry。
func RegisterJinaTools(reg tool.Registry) error {
	if _, _, exists := reg.Get(JinaSearchSpec.Name); !exists {
		if err := reg.Register(JinaSearchSpec, JinaSearchHandler()); err != nil {
			return err
		}
	}
	if _, _, exists := reg.Get(JinaReaderSpec.Name); !exists {
		if err := reg.Register(JinaReaderSpec, JinaReaderHandler()); err != nil {
			return err
		}
	}
	return nil
}

