package compiler

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func createTestTarGz(t *testing.T, files map[string]string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tarGzPath := filepath.Join(tmpDir, "test.tar.gz")

	f, err := os.Create(tarGzPath)
	if err != nil {
		t.Fatalf("failed to create tar file: %v", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write body: %v", err)
		}
	}

	return tarGzPath
}

func TestExtractTarGz_Success(t *testing.T) {
	files := map[string]string{
		"main.tex":     "\\documentclass{article}\n\\begin{document}\nHello\n\\end{document}",
		"sections/1.tex": "Section content",
	}

	tarGzPath := createTestTarGz(t, files)
	destDir := t.TempDir()

	err := ExtractTarGz(tarGzPath, destDir)
	if err != nil {
		t.Fatalf("ExtractTarGz failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(destDir, "main.tex"))
	if err != nil || string(content) != files["main.tex"] {
		t.Errorf("main.tex not extracted correctly")
	}

	subContent, err := os.ReadFile(filepath.Join(destDir, "sections", "1.tex"))
	if err != nil || string(subContent) != files["sections/1.tex"] {
		t.Errorf("sections/1.tex not extracted correctly")
	}
}

func TestExtractTarGz_ZipSlipPrevention(t *testing.T) {
	maliciousFiles := map[string]string{
		"../escaped.txt": "evil content",
	}

	tarGzPath := createTestTarGz(t, maliciousFiles)
	destDir := t.TempDir()

	err := ExtractTarGz(tarGzPath, destDir)
	if err == nil {
		t.Errorf("expected error for path traversal attempt, but got nil")
	}
}

func TestExtractErrorMessage(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	logContent := `This is pdfTeX, Version 3.141592653-2.6-1.40.24
(./main.tex
! Undefined control sequence.
l.5 \invalidcommand
`
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatalf("failed to write test log: %v", err)
	}

	errMsg := extractErrorMessage(logPath)
	expected := "! Undefined control sequence."
	if errMsg != expected {
		t.Errorf("expected %q, got %q", expected, errMsg)
	}
}
