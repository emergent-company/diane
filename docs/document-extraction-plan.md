# Document Upload & Extraction — Diane Integration Plan

## Overview

Memory Platform supports uploading files and automatically extracting typed objects (People, Companies, Tasks, etc.) from documents. This plan covers adding document listing and extraction info to the Diane companion app (`Diane.app`).

## API Surface (MP REST Endpoints)

| Endpoint | Method | Description |
|---|---|---|
| `POST /api/documents/upload` | `multipart/form-data` | Upload file (max 100MB), auto-dedup by hash |
| `GET /api/documents` | `?limit=50&cursor=...` | List documents with pagination |
| `GET /api/documents/{id}` | — | Single document metadata |
| `GET /api/documents/{id}/content` | — | Extracted text content |
| `GET /api/documents/{id}/extraction-summary` | — | Object extraction results by type |
| `GET /api/monitoring/extraction-jobs` | `?source_id={docId}&status=completed` | Extraction job details |
| `DELETE /api/documents/{id}` | — | Delete document + all related objects |

### Key Response Schemas

**Document** (from list/get):
```json
{
  "id": "uuid",
  "filename": "report.pdf",
  "mimeType": "application/pdf",
  "fileSizeBytes": 102400,
  "sourceType": "upload",
  "processingStatus": "completed",
  "conversionStatus": "completed",
  "extractionStatus": "completed",
  "objectsCreated": 5,
  "relationshipsCreated": 3,
  "chunks": 12,
  "createdAt": "2026-05-02T10:00:00Z",
  "updatedAt": "...",
  "lastExtractionAt": "..."
}
```

**ExtractionSummary** (from `/api/documents/{id}/extraction-summary`):
```json
{
  "jobId": "uuid",
  "objectsCreated": 5,
  "relationshipsCreated": 3,
  "objectsByType": {
    "Person": 2,
    "Company": 1,
    "Task": 2
  },
  "chunksProcessed": 12,
  "totalChunks": 12,
  "hasErrors": false,
  "completedAt": "2026-05-02T10:05:00Z"
}
```

## Proposed Architecture

### Layer 1: Go Backend — Local API Proxy (`local_api.go`)

**Goal**: Add document endpoints to the local API that the companion app calls.

New routes in `cmd/diane/local_api.go`:

```
GET  /api/documents              → bridge.ListDocuments(limit, cursor)
GET  /api/documents/{id}         → bridge.GetDocument(id)
GET  /api/documents/{id}/content → bridge.GetDocumentContent(id)
GET  /api/documents/{id}/extraction-summary → bridge.GetDocumentExtractionSummary(id)
POST /api/documents/upload       → bridge.UploadDocument(file, sourceType)
DELETE /api/documents/{id}       → bridge.DeleteDocument(id)
```

**Implementation approach — two options:**

**Option A: Add to Bridge (`internal/memory/bridge.go`)**
- Add `ListDocuments`, `UploadDocument`, `GetDocumentExtractionSummary` methods using raw HTTP (like the existing agent trigger pattern)
- Pro: Consistent with existing architecture, one auth config
- Con: Bridge is already large, adding document logic makes it bigger

**Option B: New DocumentClient (`internal/memory/documents.go`)**
- Separate struct that uses the same API key/project from config
- Pro: Clean separation of concerns, easier to maintain
- Con: Another abstraction layer

**Recommendation**: Option A first (minimal new files), extract to Option B if it grows.

### Layer 2: Companion App — New Views

**Goal**: Display document list and extraction details in the SwiftUI app.

**Existing patterns** (EmergentAPIClient already has `searchDocuments`/`fetchDocument`):
1. Add upload support to `EmergentAPIClient.swift`
2. Create `DocumentsViewModel.swift` for document state management
3. Create `DocumentsView.swift` — document list
4. Create `DocumentDetailView.swift` — extraction summary + objects

### Layer 3: Upload Entry Points

**Goal**: Allow file upload from the app.

**Three paths:**
1. **Drag & drop** onto the app dock icon or the Documents view
2. **File picker** (macOS `NSOpenPanel`)
3. **`diane document upload`** CLI command (for scripting)

## Implementation Plan (Tickets)

### Phase 1: Backend (Go) — ~1-2 hours

| Ticket | File(s) | Description |
|---|---|---|
| **DIA-1** Add `ListDocuments` to Bridge | `internal/memory/bridge.go` | Raw HTTP GET `/api/documents?limit=&cursor=` with auth headers. Return parsed `ListResult`. |
| **DIA-2** Add `GetDocument` + `GetDocumentContent` to Bridge | `internal/memory/bridge.go` | GET `/api/documents/{id}` and `/api/documents/{id}/content` |
| **DIA-3** Add `GetDocumentExtractionSummary` to Bridge | `internal/memory/bridge.go` | GET `/api/documents/{id}/extraction-summary`. Return `ExtractionSummary` with `objectsByType` map. |
| **DIA-4** Add `UploadDocument` to Bridge | `internal/memory/bridge.go` | POST `multipart/form-data` to `/api/documents/upload`. Handle response: 201 (created) / 200 (deduplicated). |
| **DIA-5** Add `/api/documents` routes to local API | `cmd/diane/local_api.go` | Route registration + handler functions for all 6 endpoints. |
| **DIA-6** Add `diane document` CLI subcommand | `cmd/diane/document.go` | List, upload, info, delete subcommands. |

### Phase 2: Companion App (SwiftUI) — ~2-3 hours

| Ticket | Description |
|---|---|
| **DIA-7** Add upload + extraction endpoints to `EmergentAPIClient.swift` | `uploadDocument(url:, sourceType:)`, `fetchExtractionSummary(documentID:)` |
| **DIA-8** Create `DocumentsViewModel` | ObservableObject with: documents list, polling, search/filter, upload state |
| **DIA-9** Create `DocumentsView` | List view with: filename, mimeType badge, fileSize, processingStatus icon, date |
| **DIA-10** Create `DocumentDetailView` | Extraction summary: objectsByType breakdown, job details, error summary. Navigation from list. |
| **DIA-11** Add drag-and-drop upload | AppDelegate/NSEvent handlers for file drops |
| **DIA-12** Add file picker upload | Button → NSOpenPanel → upload → show result |

### Phase 3: Wiring — ~1 hour

| Ticket | Description |
|---|---|
| **DIA-13** Navigation integration | Add Documents to sidebar/tab bar in companion app |
| **DIA-14** Extraction notification | Badge or indicator when new extraction completes (poll every 30s for in-progress docs) |

## Data Flow

```
User drags file to Diane.app
        │
        v
[Companion App] POST /api/documents/upload
        │ (multipart/form-data)
        v
[local_api.go] UploadDocument handler
        │ (raw HTTP with API key)
        v
[MP Server] Receives file, creates Document record,
            kicks off conversion + extraction pipeline
        │
        v (async processing)
[MP Server] File → text → chunks → LLM extraction → typed objects
        │
        v (poll or wait)
[Companion App] GET /api/documents/{id}
        │ → processingStatus: "completed"/"failed"
        v
[Companion App] GET /api/documents/{id}/extraction-summary
        │ → objectsByType: {"Person": 2, "Company": 1}
        v
User sees document metadata + extracted objects
```

## POC Script

A bash POC script is provided at `scripts/poc-document-extraction.sh` that exercises the MP API directly to verify:
1. File upload
2. Extraction polling
3. Extraction summary
4. Document listing
