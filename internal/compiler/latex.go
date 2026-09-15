package compiler

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CompileResult holds the output files and status of a compilation attempt.
type CompileResult struct {
	Success      bool
	PdfPath      string
	LogPath      string
	ErrorMessage string
	Duration     time.Duration
}

// ExtractTarGz securely extracts a .tar.gz archive into destDir, preventing directory traversal.
func ExtractTarGz(tarGzPath string, destDir string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return fmt.Errorf("failed to open tarball: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	destClean := filepath.Clean(destDir)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading tar entry: %w", err)
		}

		// Security: Prevent zip slip / directory traversal vulnerabilities
		target := filepath.Join(destClean, header.Name)
		targetClean := filepath.Clean(target)
		if !strings.HasPrefix(targetClean, destClean+string(filepath.Separator)) && targetClean != destClean {
			return fmt.Errorf("illegal file path in tar archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetClean, 0755); err != nil {
				return fmt.Errorf("failed to create dir %s: %w", targetClean, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetClean), 0755); err != nil {
				return fmt.Errorf("failed to create parent dir for %s: %w", targetClean, err)
			}
			outFile, err := os.OpenFile(targetClean, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", targetClean, err)
			}
			// Limit file size to 50MB per file to prevent decompression bombs
			if _, err := io.CopyN(outFile, tr, 50*1024*1024); err != nil && err != io.EOF {
				outFile.Close()
				return fmt.Errorf("failed writing file %s or exceeded size limit: %w", targetClean, err)
			}
			outFile.Close()
		}
	}

	return nil
}

// Compile executes latexmk in workDir with sandboxed, safe arguments.
func Compile(ctx context.Context, workDir string, rootFile string, engine string, timeout time.Duration) (*CompileResult, error) {
	startTime := time.Now()

	if rootFile == "" {
		rootFile = "main.tex"
	}

	// Sanitize rootFile (prevent directory traversal in filename argument)
	rootFile = filepath.Base(rootFile)
	rootPath := filepath.Join(workDir, rootFile)
	if _, err := os.Stat(rootPath); os.IsNotExist(err) {
		return &CompileResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("root file %q not found in project", rootFile),
			Duration:     time.Since(startTime),
		}, nil
	}

	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	compileCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build safe latexmk arguments
	args := []string{
		"-interaction=nonstopmode",
		"-halt-on-error",
		"-file-line-error",
		"-no-shell-escape", // CRITICAL: Disable \write18 arbitrary command execution
	}

	switch strings.ToLower(engine) {
	case "xelatex":
		args = append(args, "-xelatex")
	case "lualatex":
		args = append(args, "-lualatex")
	default: // pdflatex
		args = append(args, "-pdf")
	}

	args = append(args, rootFile)

	logFilePath := filepath.Join(workDir, "compilation.log")
	logFile, err := os.Create(logFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create compilation log file: %w", err)
	}
	defer logFile.Close()

	cmd := exec.CommandContext(compileCtx, "latexmk", args...)
	cmd.Dir = workDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Execute compilation
	execErr := cmd.Run()
	duration := time.Since(startTime)

	// Base name without .tex
	baseName := strings.TrimSuffix(rootFile, filepath.Ext(rootFile))
	pdfPath := filepath.Join(workDir, baseName+".pdf")

	result := &CompileResult{
		LogPath:  logFilePath,
		Duration: duration,
	}

	// Check if PDF was successfully created
	if fi, err := os.Stat(pdfPath); err == nil && fi.Size() > 0 && execErr == nil {
		result.Success = true
		result.PdfPath = pdfPath
		return result, nil
	}

	// Compilation failed or timed out
	result.Success = false
	if compileCtx.Err() == context.DeadlineExceeded {
		result.ErrorMessage = fmt.Sprintf("compilation timed out after %v", timeout)
	} else {
		result.ErrorMessage = extractErrorMessage(logFilePath)
		if result.ErrorMessage == "" {
			result.ErrorMessage = "LaTeX compilation failed. Check compilation.log for details."
		}
	}

	return result, nil
}

// extractErrorMessage scans the compilation log for standard LaTeX error lines.
func extractErrorMessage(logFilePath string) string {
	f, err := os.Open(logFilePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var errorLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "!") || strings.Contains(line, ": error:") || strings.Contains(line, ": Fatal error") {
			errorLines = append(errorLines, line)
			if len(errorLines) >= 3 {
				break
			}
		}
	}

	if len(errorLines) > 0 {
		return strings.Join(errorLines, " | ")
	}
	return ""
}
