package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"openleaf-compiler/internal/compiler"
	"openleaf-compiler/internal/storage"
)

// CompileHandler handles compilation requests.
type CompileHandler struct {
	storage       storage.Provider
	defaultBucket string
	authSecret    string
}

// NewCompileHandler initializes CompileHandler.
func NewCompileHandler(storage storage.Provider, defaultBucket string, authSecret string) *CompileHandler {
	return &CompileHandler{
		storage:       storage,
		defaultBucket: defaultBucket,
		authSecret:    authSecret,
	}
}

// CompileRequest defines incoming compilation payload.
type CompileRequest struct {
	JobID         string `json:"job_id"`
	ProjectID     string `json:"project_id"`
	Bucket        string `json:"bucket,omitempty"`
	TarballKey    string `json:"tarball_key"`
	RootFile      string `json:"root_file,omitempty"`
	Engine        string `json:"engine,omitempty"`
	TimeoutSec    int    `json:"timeout_sec,omitempty"`
	CallbackURL   string `json:"callback_url,omitempty"`
	CallbackToken string `json:"callback_token,omitempty"`
}

// CompileResponse defines outgoing compilation response.
type CompileResponse struct {
	JobID        string `json:"job_id"`
	ProjectID    string `json:"project_id"`
	Status       string `json:"status"` // "succeeded" or "failed"
	PdfKey       string `json:"pdf_key,omitempty"`
	LogKey       string `json:"log_key,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	DurationMs   int64  `json:"duration_ms"`
}

// HandleCompile processes a compilation job.
func (h *CompileHandler) HandleCompile(w http.ResponseWriter, r *http.Request) {
	// Optional security check on compiler endpoint
	if h.authSecret != "" {
		token := r.Header.Get("X-Compiler-Secret")
		if token == "" {
			token = r.Header.Get("Authorization")
			if len(token) > 7 && token[:7] == "Bearer " {
				token = token[7:]
			}
		}
		if token != h.authSecret {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	var req CompileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid request payload: %v"}`, err), http.StatusBadRequest)
		return
	}

	if req.JobID == "" || req.ProjectID == "" || req.TarballKey == "" {
		http.Error(w, `{"error":"missing required fields: job_id, project_id, and tarball_key are required"}`, http.StatusBadRequest)
		return
	}

	bucket := req.Bucket
	if bucket == "" {
		bucket = h.defaultBucket
	}
	if bucket == "" {
		http.Error(w, `{"error":"no storage bucket specified"}`, http.StatusBadRequest)
		return
	}

	if req.RootFile == "" {
		req.RootFile = "main.tex"
	}
	if req.Engine == "" {
		req.Engine = "pdflatex"
	}
	timeoutSec := req.TimeoutSec
	if timeoutSec <= 0 || timeoutSec > 180 {
		timeoutSec = 60
	}

	// Create isolated temporary workspace
	workDir, err := os.MkdirTemp("", fmt.Sprintf("job-%s-*", req.JobID))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to create workspace: %v"}`, err), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(workDir)

	localTarball := filepath.Join(workDir, "source.tar.gz")
	srcDir := filepath.Join(workDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to create src dir: %v"}`, err), http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	// 1. Download source tarball from storage
	if err := h.storage.Download(ctx, bucket, req.TarballKey, localTarball); err != nil {
		log.Printf("[Job %s] Storage download failed: %v", req.JobID, err)
		http.Error(w, fmt.Sprintf(`{"error":"failed to download tarball: %v"}`, err), http.StatusBadGateway)
		return
	}

	// 2. Extract tarball
	if err := compiler.ExtractTarGz(localTarball, srcDir); err != nil {
		log.Printf("[Job %s] Tarball extraction failed: %v", req.JobID, err)
		http.Error(w, fmt.Sprintf(`{"error":"failed to extract tarball: %v"}`, err), http.StatusBadRequest)
		return
	}

	// 3. Compile LaTeX
	result, err := compiler.Compile(ctx, srcDir, req.RootFile, req.Engine, time.Duration(timeoutSec)*time.Second)
	if err != nil {
		log.Printf("[Job %s] Compilation runner error: %v", req.JobID, err)
		http.Error(w, fmt.Sprintf(`{"error":"compiler runner error: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// 4. Artifact GCS Keys
	logKey := fmt.Sprintf("artifacts/%s/%s/compilation.log", req.ProjectID, req.JobID)
	pdfKey := fmt.Sprintf("artifacts/%s/%s/output.pdf", req.ProjectID, req.JobID)

	resp := CompileResponse{
		JobID:        req.JobID,
		ProjectID:    req.ProjectID,
		DurationMs:   result.Duration.Milliseconds(),
		LogKey:       logKey,
		ErrorMessage: result.ErrorMessage,
	}

	// 5. Upload build log
	if result.LogPath != "" {
		if err := h.storage.Upload(ctx, bucket, logKey, result.LogPath, "text/plain; charset=utf-8"); err != nil {
			log.Printf("[Job %s] Failed to upload log to storage: %v", req.JobID, err)
		}
	}

	// 6. Handle success vs failure
	if result.Success {
		resp.Status = "succeeded"
		resp.PdfKey = pdfKey
		if err := h.storage.Upload(ctx, bucket, pdfKey, result.PdfPath, "application/pdf"); err != nil {
			log.Printf("[Job %s] Failed to upload PDF: %v", req.JobID, err)
			resp.Status = "failed"
			resp.ErrorMessage = fmt.Sprintf("failed to store compiled PDF: %v", err)
			resp.PdfKey = ""
		}
	} else {
		resp.Status = "failed"
	}

	// 7. Dispatch callback webhook if requested
	if req.CallbackURL != "" {
		go h.dispatchCallback(req.CallbackURL, req.CallbackToken, resp)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *CompileHandler) dispatchCallback(url, token string, payload CompileResponse) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[Callback] Failed to serialize callback payload: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[Callback] Failed to create callback request: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Internal-Token", token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Callback] Error sending callback to %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("[Callback] Non-2xx response from callback %s: %d", url, resp.StatusCode)
	}
}
