package appkit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mossagi/moss/kernel/skill"
	"gopkg.in/yaml.v3"
)

// SetAppName 设置应用名称，影响全局配置目录路径。
// 必须在任何配置读写操作之前调用。
//
// 示例：
//
//	appkit.SetAppName("minicode") // 配置目录变为 ~/.minicode
func SetAppName(name string) { skill.SetAppName(name) }

// AppName 返回当前应用名称。
func AppName() string { return skill.AppName() }

// MossDir 返回全局配置目录路径（~/.<appName>）。
// 应用名称通过 SetAppName() 设置，默认为 "moss"。
func MossDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	appName := skill.AppName()
	return filepath.Join(home, "."+appName)
}

// EnsureMossDir 确保 ~/.<appName> 目录存在，不存在则创建。
// 同时会在全局配置文件不存在时创建一个可编辑的模板文件。
func EnsureMossDir() error {
	dir := MossDir()
	if dir == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	cfgPath := skill.DefaultGlobalConfigPath()
	if cfgPath == "" {
		return nil
	}

	f, err := os.OpenFile(cfgPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("create config template %s: %w", cfgPath, err)
	}
	defer f.Close()

	if _, err := f.WriteString(skill.DefaultConfigTemplate()); err != nil {
		return fmt.Errorf("write config template %s: %w", cfgPath, err)
	}

	return nil
}

// SaveConfig 将配置写入指定路径，自动创建父目录。
func SaveConfig(path string, cfg *skill.Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}
