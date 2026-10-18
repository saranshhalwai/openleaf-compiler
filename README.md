# Openleaf Compiler 🛠️

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/badge/release-v1.0.0-blue.svg)](https://github.com/saranshhalwai/openleaf-compiler/releases)

The sandboxed, asynchronous LaTeX compilation runner for **Openleaf**. Deployed as a containerized microservice on **Google Cloud Run**, executing TeX Live in an isolated runtime environment with memory caps, timeout bounds, and automated GCS storage artifact pipelines.

---

## ⚡ How It Works

1. **Triggered via Cloud Tasks**: The Openleaf Go Backend enqueues a compilation task with project metadata and tarball location.
2. **GCS Artifact Download**: The compiler fetches `source.tar.gz` from the Google Cloud Storage bucket.
3. **Sandboxed Compilation**: Unpacks files into an ephemeral workspace and executes:
   ```bash
   latexmk -pdf -interaction=nonstopmode -halt-on-error -file-line-error -synctex=1 main.tex
   ```
4. **Artifact Upload**: Uploads `output.pdf` and `compile.log` back to the designated GCS path.
5. **Webhook Callback**: Notifies the Openleaf Backend of success or failure with execution logs and PDF storage keys.

---

## 🛠️ Tech Stack

- **Runtime**: Go 1.23+ HTTP Service (`go-chi`)
- **LaTeX Engine**: `texlive-full` / `latexmk`
- **Cloud Infrastructure**: Google Cloud Run (Container Sandbox) + Google Cloud Storage + Cloud Tasks

---

## 📋 Endpoints

- `GET /health` — Liveness & readiness probe.
- `POST /compile` — Execute asynchronous LaTeX build job (invoked by Cloud Tasks with auth header).

---

## 💻 Local Development

### Prerequisites
- **Go 1.23+**
- Local LaTeX installation (`latexmk`, `pdflatex`) or Docker

### Running Locally with Docker
```bash
docker build -t openleaf-compiler .
docker run -p 8081:8080 -e USE_LOCAL_STORAGE=true openleaf-compiler
```

---

## 🚀 Production Deployment

Deployed to Google Cloud Run:

```bash
gcloud run deploy openleaf-compiler \
  --source . \
  --region asia-southeast1 \
  --memory 2Gi \
  --cpu 2 \
  --timeout 300
```

---

## 📄 License

This project is licensed under the [Apache License, Version 2.0](LICENSE).
