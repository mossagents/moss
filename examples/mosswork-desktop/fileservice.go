package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"io"
	"os"
	"path/filepath"
)

type FileService struct {
	cfg config
}

func NewFileService(cfg config) *FileService {
	return &FileService{cfg: cfg}
}

func (s *FileService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	return nil
}

func (s *FileService) ServiceShutdown() error {
	// 清理文件服务资源
	return nil
}

func (s *FileService) OpenFiles() ([]string, error) {
	app := application.Get()
	if app == nil {
		return nil, fmt.Errorf("application not ready")
	}
	return app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter("All Files", "*.*").
		AddFilter("Documents", "*.txt;*.md;*.pdf;*.doc;*.docx;*.csv;*.json;*.yaml;*.yml;*.xml;*.html").
		AddFilter("Images", "*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.svg;*.webp").
		AddFilter("Code", "*.go;*.py;*.js;*.ts;*.rs;*.java;*.cpp;*.c;*.h").
		PromptForMultipleSelection()
}

func (s *FileService) OpenImages() ([]string, error) {
	app := application.Get()
	if app == nil {
		return nil, fmt.Errorf("application not ready")
	}
	return app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter("Images", "*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.svg;*.webp").
		PromptForMultipleSelection()
}

func (s *FileService) SelectFolder() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("application not ready")
	}
	return app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
}

func (s *FileService) UploadToWorkspace(srcPath string) (string, error) {
	workspace := resolveWorkspace(s.cfg.workspace)
	filename := filepath.Base(srcPath)

	uploadsDir := filepath.Join(workspace, ".moss", "uploads")
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return "", fmt.Errorf("create uploads dir: %w", err)
	}

	dstPath := filepath.Join(uploadsDir, filename)

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

	relPath, err := filepath.Rel(workspace, dstPath)
	if err != nil {
		return dstPath, nil
	}
	return relPath, nil
}

func (s *FileService) ReadFileBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (s *FileService) GetWorkspace() string {
	return resolveWorkspace(s.cfg.workspace)
}

func (s *FileService) ListWorkspaceFiles(pattern string) ([]string, error) {
	workspace := resolveWorkspace(s.cfg.workspace)
	if pattern == "" {
		pattern = "*"
	}
	matches, err := filepath.Glob(filepath.Join(workspace, pattern))
	if err != nil {
		return nil, err
	}
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
