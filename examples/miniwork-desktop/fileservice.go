package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ─── FileService ────────────────────────────────────

// FileService 提供文件/图片上传和本地文件访问能力。
type FileService struct {
	cfg config
}

// NewFileService 创建 FileService 实例。
func NewFileService(cfg config) *FileService {
	return &FileService{cfg: cfg}
}

// ServiceStartup 在 Wails 应用启动时被调用。
func (s *FileService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return nil
}

// OpenFiles 打开系统文件选择对话框，返回选中的文件路径。
func (s *FileService) OpenFiles() ([]string, error) {
	app := application.Get()
	if app == nil {
		return nil, fmt.Errorf("application not ready")
	}

	results, err := app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter("All Files", "*.*").
		AddFilter("Documents", "*.txt;*.md;*.pdf;*.doc;*.docx;*.csv;*.json;*.yaml;*.yml;*.xml;*.html").
		AddFilter("Images", "*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.svg;*.webp").
		AddFilter("Code", "*.go;*.py;*.js;*.ts;*.rs;*.java;*.cpp;*.c;*.h").
		PromptForMultipleSelection()
	if err != nil {
		return nil, err
	}

	return results, nil
}

// OpenImages 打开系统文件选择对话框（仅限图片）。
func (s *FileService) OpenImages() ([]string, error) {
	app := application.Get()
	if app == nil {
		return nil, fmt.Errorf("application not ready")
	}

	results, err := app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter("Images", "*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.svg;*.webp").
		PromptForMultipleSelection()
	if err != nil {
		return nil, err
	}

	return results, nil
}

// SelectFolder 打开系统文件夹选择对话框。
func (s *FileService) SelectFolder() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("application not ready")
	}

	result, err := app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}

	return result, nil
}

// UploadToWorkspace 将文件复制到 workspace 目录。
// 返回文件在 workspace 中的相对路径。
func (s *FileService) UploadToWorkspace(srcPath string) (string, error) {
	workspace := resolveWorkspace(s.cfg.workspace)
	filename := filepath.Base(srcPath)

	// 创建 uploads 目录
	uploadsDir := filepath.Join(workspace, ".moss", "uploads")
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return "", fmt.Errorf("create uploads dir: %w", err)
	}

	dstPath := filepath.Join(uploadsDir, filename)

	// 复制文件
	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("open source file: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return "", fmt.Errorf("create destination file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("copy file: %w", err)
	}

	// 返回相对路径
	relPath, err := filepath.Rel(workspace, dstPath)
	if err != nil {
		return dstPath, nil
	}
	return relPath, nil
}

// ReadFileBase64 读取文件并返回 base64 编码内容。
// 用于在前端预览图片等。
func (s *FileService) ReadFileBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// GetWorkspace 返回当前工作目录。
func (s *FileService) GetWorkspace() string {
	return resolveWorkspace(s.cfg.workspace)
}

// ListWorkspaceFiles 列出 workspace 下的文件。
func (s *FileService) ListWorkspaceFiles(pattern string) ([]string, error) {
	workspace := resolveWorkspace(s.cfg.workspace)
	if pattern == "" {
		pattern = "*"
	}

	matches, err := filepath.Glob(filepath.Join(workspace, pattern))
	if err != nil {
		return nil, err
	}

	// 转换为相对路径
	result := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, err := filepath.Rel(workspace, m)
		if err != nil {
			result = append(result, m)
		} else {
			result = append(result, rel)
		}
	}
	return result, nil
}
