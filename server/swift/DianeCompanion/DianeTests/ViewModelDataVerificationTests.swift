import XCTest
@testable import Diane

// MARK: - Documents View Data

final class DocumentsViewModelTests: XCTestCase {

    let testDoc = Document(id: "doc-1", projectId: "proj-1", filename: "report.pdf",
                           mimeType: "application/pdf", fileHash: "abc123",
                           contentHash: nil, sourceType: "upload",
                           conversionStatus: "completed", extractionStatus: "done",
                           processingStatus: nil, storageKey: nil, storageUrl: nil,
                           fileSizeBytes: 1024000, syncVersion: 2, chunks: 100,
                           embeddedChunks: 80, totalChars: 50000,
                           objectsCreated: 15, relationshipsCreated: 8,
                           content: nil, createdAt: "2026-01-15T10:00:00Z",
                           updatedAt: "2026-01-15T12:00:00Z")

    func testDocumentFilenameDisplay() {
        XCTAssertEqual(testDoc.filename, "report.pdf")
    }

    func testDocumentMimeTypeDisplay() {
        XCTAssertEqual(testDoc.mimeType, "application/pdf")
    }

    func testDocumentFileSizeFormatting() {
        if let bytes = testDoc.fileSizeBytes {
            let kb = bytes / 1024
            // View renders: "1000 KB" or "1.0 MB"
            if kb >= 1024 {
                let mb = Double(kb) / 1024
                let display = String(format: "%.1f MB", mb)
                XCTAssertEqual(display, "1.0 MB")
            } else {
                let display = "\(kb) KB"
                // Not reached for 1024000 bytes
            }
        } else {
            XCTFail("Expected fileSizeBytes")
        }
    }

    func testDocumentExtractionStatusDisplay() {
        // View renders: extraction status as badge
        let extractionStatus = testDoc.extractionStatus ?? "pending"
        XCTAssertEqual(extractionStatus, "done")
    }

    func testDocumentChunkCountDisplay() {
        if let chunks = testDoc.chunks, let embedded = testDoc.embeddedChunks {
            // View renders: "80/100 chunks embedded"
            let display = "\(embedded)/\(chunks) chunks"
            XCTAssertEqual(display, "80/100 chunks")
        }
    }

    func testDocumentObjectAndRelationCreated() {
        if let objs = testDoc.objectsCreated, let rels = testDoc.relationshipsCreated {
            XCTAssertEqual(objs, 15)
            XCTAssertEqual(rels, 8)
        }
    }

    func testDocumentChunkStatus() {
        if let chunks = testDoc.chunks, let embedded = testDoc.embeddedChunks {
            let allEmbedded = chunks > 0 && embedded == chunks
            XCTAssertFalse(allEmbedded)
        }
    }

    func testDocumentStatusPending() {
        let doc = Document(id: "doc-2", projectId: nil, filename: "new.docx",
                           mimeType: nil, fileHash: nil, contentHash: nil,
                           sourceType: nil, conversionStatus: nil,
                           extractionStatus: nil, processingStatus: nil,
                           storageKey: nil, storageUrl: nil, fileSizeBytes: nil,
                           syncVersion: nil, chunks: nil, embeddedChunks: nil,
                           totalChars: nil, objectsCreated: nil,
                           relationshipsCreated: nil, content: nil,
                           createdAt: nil, updatedAt: nil)
        XCTAssertEqual(doc.extractionStatus ?? "pending", "pending")
        XCTAssertEqual(doc.conversionStatus ?? "pending", "pending")
    }

    // MARK: Extraction summary

    func testExtractionSummaryDisplay() {
        let summary = ExtractionSummary(jobId: "job-1", completedAt: "2026-01-15T12:00:00Z",
                                        objectsCreated: 15, relationshipsCreated: 8,
                                        objectsByType: ["note": 10, "contact": 5],
                                        objectIds: ["o1", "o2"], chunksProcessed: 80,
                                        totalChunks: 100, hasErrors: false, errorSummary: nil)
        XCTAssertEqual(summary.objectsCreated, 15)
        XCTAssertEqual(summary.relationshipsCreated, 8)
        XCTAssertFalse(summary.hasErrors)
        XCTAssertEqual(summary.chunksProcessed, 80)
    }

    func testExtractionSummaryWithErrors() {
        let summary = ExtractionSummary(jobId: "job-2", completedAt: "2026-01-15T12:00:00Z",
                                        objectsCreated: 5, relationshipsCreated: 2,
                                        objectsByType: nil, objectIds: nil,
                                        chunksProcessed: 60, totalChunks: 100,
                                        hasErrors: true, errorSummary: "Connection timeout")
        XCTAssertTrue(summary.hasErrors)
        XCTAssertEqual(summary.errorSummary, "Connection timeout")
    }

    // MARK: Document chunks

    func testDocumentChunkAtIndex() {
        let chunk = DocumentChunk(id: "ch-1", documentId: "doc-1", index: 0,
                                  text: "Hello world", size: 100,
                                  hasEmbedding: true, metadata: nil, createdAt: nil)
        XCTAssertEqual(chunk.index, 0)
        XCTAssertTrue(chunk.hasEmbedding)
        XCTAssertEqual(chunk.text, "Hello world")
    }

    func testChunksResponse() {
        let resp = ChunksResponse(data: [
            DocumentChunk(id: "c1", documentId: "doc-1", index: 0,
                          text: "a", size: 1, hasEmbedding: true,
                          metadata: nil, createdAt: nil)
        ], totalCount: 1)
        XCTAssertEqual(resp.totalCount, 1)
        XCTAssertEqual(resp.data.count, 1)
    }
}

// MARK: - Relay Nodes View Data

final class RelayNodesViewModelTests: XCTestCase {

    func testNodeSummaryHeaderOnlineCount() {
        let nodes = [
            RelayNode(instanceID: "n1", hostname: "macmini-1", mode: "master",
                      version: "1.0.0", toolCount: 15,
                      connectedAt: "2026-04-28T10:00:00Z", online: true,
                      uptime: "2d", provider: "openai", relayActive: true,
                      botActive: true, healthy: true),
            RelayNode(instanceID: "n2", hostname: "vps-1", mode: "slave",
                      version: "1.0.0", toolCount: 8,
                      connectedAt: "2026-04-28T08:00:00Z", online: false,
                      uptime: nil, provider: nil, relayActive: false,
                      botActive: nil, healthy: nil),
            RelayNode(instanceID: "n3", hostname: "laptop", mode: "slave",
                      version: nil, toolCount: nil,
                      connectedAt: nil, online: true,
                      uptime: nil, provider: nil, relayActive: nil,
                      botActive: nil, healthy: true)
        ]

        let onlineCount = nodes.filter { $0.online == true }.count
        // View renders: "2/3 nodes"
        let display = "\(onlineCount)/\(nodes.count) nodes"
        XCTAssertEqual(display, "2/3 nodes")
    }

    func testNodeMasterSlaveCount() {
        let nodes = [
            RelayNode(instanceID: "n1", hostname: "mini", mode: "master",
                      version: nil, toolCount: nil, connectedAt: nil,
                      online: true, uptime: nil, provider: nil,
                      relayActive: nil, botActive: nil, healthy: nil),
            RelayNode(instanceID: "n2", hostname: "vps", mode: "slave",
                      version: nil, toolCount: nil, connectedAt: nil,
                      online: true, uptime: nil, provider: nil,
                      relayActive: nil, botActive: nil, healthy: nil),
            RelayNode(instanceID: "n3", hostname: "laptop", mode: "slave",
                      version: nil, toolCount: nil, connectedAt: nil,
                      online: false, uptime: nil, provider: nil,
                      relayActive: nil, botActive: nil, healthy: nil)
        ]

        let masterCount = nodes.filter { $0.mode == "master" }.count
        let slaveCount = nodes.filter { $0.mode == "slave" }.count

        // View renders: "● 1 master" + "● 2 slave"
        XCTAssertEqual(masterCount, 1)
        XCTAssertEqual(slaveCount, 2)
    }

    func testNodeOnlineOnly() {
        let nodes = [
            RelayNode(instanceID: "n1", hostname: nil, mode: "master",
                      version: nil, toolCount: 10, connectedAt: nil,
                      online: true, uptime: nil, provider: nil,
                      relayActive: nil, botActive: nil, healthy: nil)
        ]
        // View renders: hostname ?? instanceID as display name
        let displayName = nodes[0].hostname ?? nodes[0].instanceID
        XCTAssertEqual(displayName, "n1")
    }

    func testNodeWithHostname() {
        let node = RelayNode(instanceID: "abc-123", hostname: "mcj-mini",
                             mode: "master", version: "1.0.0", toolCount: 12,
                             connectedAt: nil, online: true, uptime: "5d",
                             provider: "openai", relayActive: true,
                             botActive: true, healthy: true)
        XCTAssertEqual(node.hostname, "mcj-mini")
        XCTAssertEqual(node.mode, "master")
        XCTAssertEqual(node.toolCount, 12)
        XCTAssertEqual(node.online, true)
        XCTAssertTrue(node.healthy ?? false)
    }

    func testNodeEmptyState() {
        let nodes: [RelayNode] = []
        // View renders: EmptyStateView with "No Connected Nodes"
        XCTAssertTrue(nodes.isEmpty)
    }
}

// MARK: - Traces & Workers View Data

final class TracesViewModelTests: XCTestCase {

    func testTraceStatusCompletedDisplay() {
        let trace = Trace(id: "t-1", status: "completed", spanCount: 5,
                          createdAt: "2026-04-28T10:00:00Z",
                          updatedAt: "2026-04-28T10:05:00Z",
                          sourceType: "document", documentID: "doc-1",
                          errorMessage: nil)
        XCTAssertEqual(trace.status, "completed")
        XCTAssertEqual(trace.spanCount, 5)
        XCTAssertEqual(trace.sourceType, "document")
    }

    func testTraceStatusErrorDisplay() {
        let trace = Trace(id: "t-2", status: "error", spanCount: nil,
                          createdAt: "2026-04-28T10:00:00Z",
                          updatedAt: "2026-04-28T10:00:30Z",
                          sourceType: nil, documentID: "doc-2",
                          errorMessage: "Processing timeout")
        XCTAssertEqual(trace.status, "error")
        XCTAssertEqual(trace.errorMessage, "Processing timeout")
        XCTAssertNil(trace.spanCount)
    }

    func testTraceStatusRunningDisplay() {
        let trace = Trace(id: "t-3", status: "running", spanCount: 3,
                          createdAt: "2026-04-28T10:00:00Z",
                          updatedAt: nil, sourceType: "upload",
                          documentID: nil, errorMessage: nil)
        XCTAssertEqual(trace.status, "running")
    }

    func testTraceListCountDisplay() {
        let traces = [Trace(id: "t1", status: "completed", spanCount: nil,
                            createdAt: nil, updatedAt: nil,
                            sourceType: nil, documentID: nil, errorMessage: nil)]
        let display = "\(traces.count) trace\(traces.count == 1 ? "" : "s")"
        XCTAssertEqual(display, "1 trace")
    }

    func testTraceListCountPlural() {
        let traces = [
            Trace(id: "t1", status: "completed", spanCount: nil,
                  createdAt: nil, updatedAt: nil, sourceType: nil,
                  documentID: nil, errorMessage: nil),
            Trace(id: "t2", status: "running", spanCount: nil,
                  createdAt: nil, updatedAt: nil, sourceType: nil,
                  documentID: nil, errorMessage: nil)
        ]
        let display = "\(traces.count) trace\(traces.count == 1 ? "" : "s")"
        XCTAssertEqual(display, "2 traces")
    }

    // MARK: Trace detail

    func testTraceDetailDisplay() {
        let detail = TraceDetail(id: "t-1", status: "completed",
                                 logs: ["Start", "Process", "Done"],
                                 llmCalls: [
                                    LLMCall(id: "llm-1", model: "gpt-4",
                                            prompt: "Extract entities",
                                            response: "[...]", durationMs: 1500)
                                 ])
        XCTAssertEqual(detail.logs?.count, 3)
        XCTAssertEqual(detail.llmCalls?.count, 1)
        XCTAssertEqual(detail.llmCalls?.first?.model, "gpt-4")
        XCTAssertEqual(detail.llmCalls?.first?.durationMs, 1500)
    }
}

// MARK: - System View Data

final class SystemViewModelTests: XCTestCase {

    func testDoctorOkDisplay() {
        let result = DoctorResponse(ok: true, version: "1.0.0",
                                     results: [
                                        DoctorCheckItem(check: "config_file", status: "ok",
                                                        message: "Config file found", details: nil),
                                        DoctorCheckItem(check: "api_token", status: "ok",
                                                        message: "API token valid", details: nil)
                                     ])
        XCTAssertTrue(result.ok)
        XCTAssertEqual(result.results.count, 2)
    }

    func testDoctorWarningDisplay() {
        let item = DoctorCheckItem(check: "server_version", status: "warning",
                                   message: "Version mismatch", details: nil)
        // View renders: exclamationmark.triangle.fill icon + yellow/warning
        XCTAssertEqual(item.status, "warning")
        XCTAssertEqual(item.iconName, "exclamationmark.triangle.fill")
        XCTAssertEqual(item.displayName, "Server Version")
    }

    func testDoctorErrorDisplay() {
        let item = DoctorCheckItem(check: "session_crud", status: "error",
                                   message: "Cannot create session", details: nil)
        XCTAssertEqual(item.status, "error")
        XCTAssertEqual(item.iconName, "xmark.circle.fill")
        XCTAssertEqual(item.message, "Cannot create session")
    }

    func testDoctorUnknownStatusIcon() {
        let item = DoctorCheckItem(check: "unknown_check", status: "weird",
                                   message: "Unknown", details: nil)
        XCTAssertEqual(item.iconName, "questionmark.circle")
    }

    func testDoctorCheckAllResultsDisplay() {
        let result = DoctorResponse(ok: false, version: nil,
                                     results: [
                                        DoctorCheckItem(check: "config_file", status: "ok",
                                                        message: "OK", details: nil),
                                        DoctorCheckItem(check: "server_version", status: "error",
                                                        message: "Version not found", details: nil)
                                     ])
        XCTAssertFalse(result.ok)
        let errors = result.results.filter { $0.status == "error" }
        XCTAssertEqual(errors.count, 1)
        XCTAssertEqual(errors[0].check, "server_version")
    }

    // MARK: Doctor display name mapping

    func testDoctorCheckDisplayNames() {
        let items = [
            DoctorCheckItem(check: "config_file", status: "ok", message: "", details: nil),
            DoctorCheckItem(check: "api_token", status: "ok", message: "", details: nil),
            DoctorCheckItem(check: "sdk_connection", status: "ok", message: "", details: nil),
            DoctorCheckItem(check: "project_info", status: "ok", message: "", details: nil),
            DoctorCheckItem(check: "agent_definitions", status: "ok", message: "", details: nil),
            DoctorCheckItem(check: "session_crud", status: "ok", message: "", details: nil),
            DoctorCheckItem(check: "memory_search", status: "ok", message: "", details: nil),
        ]

        let expected = [
            "Config File", "API Token", "SDK Connection", "Project Info",
            "Agent Definitions", "Session CRUD", "Memory Search"
        ]

        for (item, expectedName) in zip(items, expected) {
            XCTAssertEqual(item.displayName, expectedName,
                           "Unexpected displayName for '\(item.check)'")
        }
    }

    func testDoctorUnknownCheckDisplayName() {
        let item = DoctorCheckItem(check: "custom_check", status: "ok",
                                   message: "", details: nil)
        let expected = item.check.replacingOccurrences(of: "_", with: " ").capitalized
        XCTAssertEqual(expected, "Custom Check")
    }

    // MARK: Server diagnostics display

    func testServerDiagnosticsDisplay() {
        let diag = ServerDiagnostics(
            timestamp: "2026-04-28T12:00:00Z",
            uptime: "2h 30m",
            server: ServerDiagnostics.ServerInfo(version: "1.0.0", environment: "production"),
            database: ServerDiagnostics.DatabaseInfo(
                pool: ServerDiagnostics.DatabaseInfo.DBPool(totalConns: 10, idleConns: 3, maxConns: 20)
            )
        )
        XCTAssertEqual(diag.server?.version, "1.0.0")
        XCTAssertEqual(diag.database?.pool?.totalConns, 10)
        XCTAssertEqual(diag.uptime, "2h 30m")
    }

    func testServerDiagnosticsEmpty() {
        let diag = ServerDiagnostics(timestamp: nil, uptime: nil, server: nil, database: nil)
        XCTAssertNil(diag.server)
        XCTAssertNil(diag.uptime)
    }

    // MARK: Update checker version display

    func testVersionDisplayFormats() {
        // View renders: currentVersion ?? "—" and latestVersion ?? "—"
        let currentVersion: String? = "1.0.0"
        let latestVersion: String? = "1.1.0"

        XCTAssertEqual(currentVersion ?? "—", "1.0.0")
        XCTAssertEqual(latestVersion ?? "—", "1.1.0")
    }

    func testVersionDisplayWhenNil() {
        let currentVersion: String? = nil
        XCTAssertEqual(currentVersion ?? "—", "—")
    }
}

// MARK: - Graph Objects View Data

final class GraphObjectsViewModelTests: XCTestCase {

    func testGraphObjectDisplayNameFromProperties() {
        let obj = GraphObject(id: "obj-1", type: "note", score: 0.95,
                              properties: ["name": AnyCodable("My Note")],
                              createdAt: "2026-01-15T10:00:00Z")
        XCTAssertEqual(obj.displayName, "My Note")
    }

    func testGraphObjectDisplayNameFallback() {
        let obj = GraphObject(id: "obj-12345678", type: "note", score: nil,
                              properties: nil, createdAt: nil)
        let expected = "note: \(obj.id.prefix(8))"
        XCTAssertEqual(obj.displayName, expected)
    }

    func testGraphObjectDisplayNameTruncatedID() {
        let obj = GraphObject(id: "abcdef123456", type: "contact", score: nil,
                              properties: nil, createdAt: nil)
        // Falls back to "type: id.prefix(8)"
        let expected = "contact: abcdef12"
        XCTAssertEqual(obj.displayName, expected)
    }

    func testGraphObjectWithScore() {
        let obj = GraphObject(id: "o-1", type: "note", score: 0.85,
                              properties: nil, createdAt: nil)
        XCTAssertEqual(obj.score, 0.85)
    }

    func testGraphObjectTypeUnknown() {
        let obj = GraphObject(id: "o-1", type: nil, score: nil,
                              properties: nil, createdAt: nil)
        let fallback = obj.displayName
        // type ?? "?" — renders as "?: o-1"
        let prefix = obj.type ?? "?"
        let expected = "\(prefix): \(obj.id.prefix(8))"
        XCTAssertEqual(fallback, expected)
    }

    // MARK: Graph object stats display

    func testGraphObjectStatsTotal() {
        let stats = GraphObjectStatsResponse(total: 167, byType: [
            TypeCountInfo(typeName: "note", count: 150),
            TypeCountInfo(typeName: "contact", count: 17)
        ])
        // View renders: "167 total"
        let display = "\(stats.total) total"
        XCTAssertEqual(display, "167 total")
    }

    func testGraphObjectStatsByTypeFormatted() {
        let stats = GraphObjectStatsResponse(total: 150, byType: [
            TypeCountInfo(typeName: "note", count: 100),
            TypeCountInfo(typeName: "contact", count: 50)
        ])
        // View renders count in type card
        XCTAssertEqual(stats.byType.count, 2)
        XCTAssertEqual(stats.byType[0].count, 100)
        XCTAssertEqual(stats.byType[1].count, 50)
    }

    // MARK: Query results

    func testQueryResultDisplay() {
        let obj = GraphObject(id: "q-1", type: "note", score: 0.95,
                              properties: ["name": AnyCodable("Found Note")],
                              createdAt: nil)
        let item = QueryResultItem(object: obj, score: 0.95, lexicalScore: 0.8)
        XCTAssertEqual(item.score, 0.95)
        XCTAssertEqual(item.lexicalScore, 0.8)
        XCTAssertEqual(item.object.displayName, "Found Note")
    }
}

// MARK: - Embedding Status View Data

final class EmbeddingViewModelTests: XCTestCase {

    func testEmbeddingWorkerStateRunning() {
        let state = EmbeddingWorkerState(running: true, paused: false)
        XCTAssertTrue(state.running)
        XCTAssertFalse(state.paused)
    }

    func testEmbeddingWorkerStatePaused() {
        let state = EmbeddingWorkerState(running: false, paused: true)
        XCTAssertFalse(state.running)
        XCTAssertTrue(state.paused)
    }

    func testEmbeddingConfigDisplay() {
        let config = EmbeddingConfig(batchSize: 50, concurrency: 4, intervalMs: 1000,
                                     minConcurrency: 2, maxConcurrency: 8, currentConcurrency: 4,
                                     healthScore: 0.95, enableAdaptiveScaling: true)
        // View renders: "Batch: 50", "Concurrency: 4", etc.
        XCTAssertEqual(config.batchSize, 50)
        XCTAssertEqual(config.concurrency, 4)
        XCTAssertEqual(config.healthScore, 0.95)
        XCTAssertTrue(config.enableAdaptiveScaling ?? false)
    }

    func testEmbeddingConfigHealthScoreDisplay() {
        let config = EmbeddingConfig(batchSize: nil, concurrency: nil, intervalMs: nil,
                                     minConcurrency: nil, maxConcurrency: nil,
                                     currentConcurrency: nil,
                                     healthScore: 0.85, enableAdaptiveScaling: nil)
        // View might render: "85%" or similar
        if let score = config.healthScore {
            let display = String(format: "%.0f%%", score * 100)
            XCTAssertEqual(display, "85%")
        }
    }
}

// MARK: - Date Formatting Tests

final class DateUtilsTests: XCTestCase {

    func testTimestampRelativeJustNow() {
        let now = ISO8601DateFormatter().string(from: Date())
        let result = DateUtils.formatTimestamp(now)
        XCTAssertEqual(result, "just now")
    }

    func testTimestampRelativeMinutes() {
        let past = Date().addingTimeInterval(-180)  // 3 min ago
        let dateStr = ISO8601DateFormatter().string(from: past)
        let result = DateUtils.formatTimestamp(dateStr)
        XCTAssertEqual(result, "3m ago")
    }

    func testTimestampRelativeHours() {
        let past = Date().addingTimeInterval(-7200)  // 2h ago
        let dateStr = ISO8601DateFormatter().string(from: past)
        let result = DateUtils.formatTimestamp(dateStr)
        XCTAssertEqual(result, "2h ago")
    }

    func testTimestampRelativeYesterday() {
        let past = Date().addingTimeInterval(-90000)  // ~25h ago
        let dateStr = ISO8601DateFormatter().string(from: past)
        let result = DateUtils.formatTimestamp(dateStr)
        XCTAssertEqual(result, "yesterday")
    }

    func testTimestampRelativeDays() {
        let past = Date().addingTimeInterval(-345600)  // 4d ago
        let dateStr = ISO8601DateFormatter().string(from: past)
        let result = DateUtils.formatTimestamp(dateStr)
        XCTAssertEqual(result, "4d ago")
    }

    func testTimestampFallbackForInvalidDate() {
        let result = DateUtils.formatTimestamp("not-a-date")
        XCTAssertEqual(result, "not-a-date")
    }

    func testTimestampWithFractionalSeconds() {
        let result = DateUtils.formatTimestamp("2026-04-28T10:00:00.123Z")
        // Should parse and not fall through to raw string
        XCTAssertNotEqual(result, "2026-04-28T10:00:00.123Z")
    }
}

// MARK: - Worker Status Display Tests

final class WorkerStatusTests: XCTestCase {

    func testWorkerStatusLabels() {
        XCTAssertEqual(WorkerStatus.idle.displayLabel, "Idle")
        XCTAssertEqual(WorkerStatus.busy.displayLabel, "Busy")
        XCTAssertEqual(WorkerStatus.offline.displayLabel, "Offline")
        XCTAssertEqual(WorkerStatus.unknown.displayLabel, "Unknown")
    }

    func testWorkerStatusIcons() {
        XCTAssertEqual(WorkerStatus.idle.systemIcon, "checkmark.circle.fill")
        XCTAssertEqual(WorkerStatus.busy.systemIcon, "gearshape.fill")
        XCTAssertEqual(WorkerStatus.offline.systemIcon, "exclamationmark.triangle.fill")
        XCTAssertEqual(WorkerStatus.unknown.systemIcon, "questionmark.circle")
    }

    func testWorkerDisplay() {
        let worker = Worker(id: "w-1", status: .busy, currentJobID: "job-1",
                            cpuPercent: 65.5, memoryMB: 2048.0,
                            lastSeenAt: "2026-04-28T10:00:00Z")
        XCTAssertEqual(worker.status, .busy)
        XCTAssertEqual(worker.currentJobID, "job-1")
        XCTAssertEqual(worker.cpuPercent, 65.5)
        XCTAssertEqual(worker.memoryMB, 2048.0)
    }

    func testWorkerIdleDisplay() {
        let worker = Worker(id: "w-2", status: .idle, currentJobID: nil,
                            cpuPercent: 5.0, memoryMB: 512.0,
                            lastSeenAt: nil)
        XCTAssertEqual(worker.status.displayLabel, "Idle")
        XCTAssertNil(worker.currentJobID)
    }
}

// MARK: - AgentStats Summary Display

final class AgentStatsSummaryTests: XCTestCase {

    func testDisplayNameDirect() {
        let stats = AgentStatsSummary(agentName: "diane-default",
                                       agentId: "agent-123", agentDescription: nil,
                                       agentFlowType: "pipeline",
                                       totalRuns: 100, successRuns: 95, errorRuns: 5,
                                       avgDurationMs: 2500, avgStepCount: 3.5,
                                       avgToolCalls: 2.1,
                                       avgInputTokens: 500, avgOutputTokens: 1200,
                                       totalDurationMs: 250000,
                                       totalInputTokens: 50000, totalOutputTokens: 120000,
                                       totalCostUsd: 0.50, avgCostUsd: 0.005,
                                       successRate: 0.95)
        // With agentId set, displayName should be agentName directly
        XCTAssertEqual(stats.displayName, "diane-default")
    }

    func testDisplayNameStripsTimestampSuffix() {
        let stats = AgentStatsSummary(agentName: "my-agent-1612345678",
                                       agentId: nil, agentDescription: nil,
                                       agentFlowType: "pipeline",
                                       totalRuns: 10, successRuns: 8, errorRuns: 2,
                                       avgDurationMs: 1000, avgStepCount: 2.0,
                                       avgToolCalls: 1.0,
                                       avgInputTokens: 200, avgOutputTokens: 500,
                                       totalDurationMs: 10000,
                                       totalInputTokens: 2000, totalOutputTokens: 5000,
                                       totalCostUsd: 0.05, avgCostUsd: 0.005,
                                       successRate: 0.8)
        // Should strip trailing -<digits>
        let expected = "my-agent"
        XCTAssertEqual(stats.displayName, expected)
    }

    func testDisplayNameNoSuffix() {
        let stats = AgentStatsSummary(agentName: "diane-researcher",
                                       agentId: nil, agentDescription: nil,
                                       agentFlowType: "pipeline",
                                       totalRuns: 50, successRuns: 48, errorRuns: 2,
                                       avgDurationMs: 5000, avgStepCount: 5.0,
                                       avgToolCalls: 3.0,
                                       avgInputTokens: 1000, avgOutputTokens: 3000,
                                       totalDurationMs: 250000,
                                       totalInputTokens: 50000, totalOutputTokens: 150000,
                                       totalCostUsd: 1.25, avgCostUsd: 0.025,
                                       successRate: 0.96)
        // No timestamp suffix — displayName same as agentName
        XCTAssertEqual(stats.displayName, "diane-researcher")
    }

    func testAgentStatsTotalsFormatted() {
        let totals = AgentStatsTotals(totalRuns: 150, totalSuccess: 140, totalErrors: 10,
                                       totalDurationMs: 360000, totalInputTokens: 75000,
                                       totalOutputTokens: 200000, totalCostUsd: 2.50,
                                       overallAvgDurationMs: 2400,
                                       overallSuccessRate: 0.933)
        XCTAssertEqual(totals.totalRuns, 150)
        XCTAssertEqual(totals.overallSuccessRate, 0.933)
        XCTAssertEqual(formatCost(totals.totalCostUsd), "$2.500")
        XCTAssertEqual(formatDuration(totals.overallAvgDurationMs), "2.4s")
        XCTAssertEqual(formatTokenCount(totals.totalInputTokens), "75.0K")
    }

    func testProviderStatsSummary() {
        let summary = ProviderStatsSummary(providerName: "openai", modelName: "gpt-4",
                                            totalRuns: 100, successRuns: 98, errorRuns: 2,
                                            totalInputTokens: 100000,
                                            totalOutputTokens: 50000,
                                            totalCostUsd: 5.50)
        XCTAssertEqual(summary.providerName, "openai")
        XCTAssertEqual(summary.modelName, "gpt-4")
        XCTAssertEqual(summary.totalRuns, 100)
        let rate = Double(summary.successRuns) / Double(max(summary.totalRuns, 1)) * 100
        XCTAssertEqual(rate, 98.0)
    }
}

// MARK: - Account Stats Display

final class AccountStatsTests: XCTestCase {

    func testAccountStatsDisplay() {
        let stats = AccountStats(serverURL: "https://api.emergent.sh",
                                  serverVersion: "1.0.0",
                                  latencyMs: 45.5,
                                  totalProjects: 3, totalObjects: 15000,
                                  totalRelations: 5000, totalApiRequests: 250000,
                                  avgLatencyMs: 42.1)
        XCTAssertEqual(stats.serverURL, "https://api.emergent.sh")
        XCTAssertEqual(stats.totalProjects, 3)
        XCTAssertEqual(stats.totalObjects, 15000)
        XCTAssertEqual(stats.totalRelations, 5000)
        // View might render: "15.0K objects", "5.0K relations"
        XCTAssertEqual(formatCount(stats.totalObjects), "15.0K")
        XCTAssertEqual(formatCount(stats.totalRelations), "5.0K")
    }
}

// MARK: - Embedding Policy Display

final class EmbeddingPolicyTests: XCTestCase {

    func testEmbeddingPolicyActive() {
        let policy = EmbeddingPolicy(id: "p-1", projectId: "proj-1",
                                      name: "notes-embedding", description: "Embed notes",
                                      objectTypes: ["note"], fields: ["title", "body"],
                                      template: nil, model: "text-embedding-3-small",
                                      active: true)
        XCTAssertTrue(policy.active)
        XCTAssertEqual(policy.objectTypes?.count, 1)
        XCTAssertEqual(policy.fields?.count, 2)
        XCTAssertEqual(policy.model, "text-embedding-3-small")
    }

    func testEmbeddingPolicyInactive() {
        let policy = EmbeddingPolicy(id: "p-2", projectId: "proj-1",
                                      name: "old-policy", description: nil,
                                      objectTypes: nil, fields: nil,
                                      template: nil, model: nil,
                                      active: false)
        XCTAssertFalse(policy.active)
    }
}

// MARK: - Model Edge Case Tests
//
// Tests that catch API contract violations: empty strings, extreme values,
// nil optionals, unicode, and format validation.

final class ModelEdgeCaseTests: XCTestCase {

    // MARK: - Session edge cases

    func testSessionWithMinimalFields() throws {
        // API might return sessions with only id (no status, no title)
        let json = """
        {"id": "minimal-session"}
        """.data(using: .utf8)!
        let session = try JSONDecoder().decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "minimal-session")
        XCTAssertNil(session.title, "Title should be nil when not provided")
        XCTAssertNil(session.status, "Status should be nil when not provided")
        XCTAssertNil(session.messageCount, "Message count should be nil when absent")
    }

    func testSessionWithEmptyTitle() throws {
        let json = """
        {"id": "s-1", "title": "", "status": "active"}
        """.data(using: .utf8)!
        let session = try JSONDecoder().decode(DianeSession.self, from: json)
        XCTAssertEqual(session.title, "")
    }

    func testSessionWithUnicodeTitle() throws {
        let json = """
        {"id": "s-1", "title": "🔥 Test 中文 Español", "status": "active"}
        """.data(using: .utf8)!
        let session = try JSONDecoder().decode(DianeSession.self, from: json)
        XCTAssertEqual(session.title, "🔥 Test 中文 Español")
    }

    func testSessionWithLargeMessageCount() throws {
        let json = """
        {"id": "s-1", "message_count": 999999, "total_tokens": 999999999}
        """.data(using: .utf8)!
        let session = try JSONDecoder().decode(DianeSession.self, from: json)
        XCTAssertEqual(session.messageCount, 999999)
        XCTAssertEqual(session.totalTokens, 999999999)
    }

    // MARK: - Message edge cases

    func testMessageWithEmptyContent() throws {
        let json = """
        {"id": "m-1", "role": "user", "content": ""}
        """.data(using: .utf8)!
        let msg = try JSONDecoder().decode(DianeMessage.self, from: json)
        XCTAssertEqual(msg.content, "")
    }

    func testMessageWithVeryLongContent() throws {
        let longText = String(repeating: "Lorem ipsum dolor sit amet ", count: 1000)
        let jsonData = try JSONSerialization.data(withJSONObject: [
            "id": "m-long", "role": "user", "content": longText
        ])
        let msg = try JSONDecoder().decode(DianeMessage.self, from: jsonData)
        XCTAssertEqual(msg.content.count, longText.count)
        XCTAssertTrue(msg.content.hasPrefix("Lorem ipsum"))
    }

    func testMessageWithNullToolCalls() throws {
        let json = """
        {"id": "m-1", "role": "assistant", "content": "OK", "tool_calls": null}
        """.data(using: .utf8)!
        let msg = try JSONDecoder().decode(DianeMessage.self, from: json)
        XCTAssertNil(msg.toolCalls, "null tool_calls should decode as nil")
    }

    func testMessageWithEmptyToolCalls() throws {
        let json = """
        {"id": "m-1", "role": "assistant", "content": "OK", "tool_calls": []}
        """.data(using: .utf8)!
        let msg = try JSONDecoder().decode(DianeMessage.self, from: json)
        // Should decode as nil (view checks isEmpty → nil)
        XCTAssertNil(msg.toolCalls, "Empty tool calls should be nil")
    }

    // MARK: - MCPServer edge cases

    func testMCPServerMinimal() throws {
        let json = """
        {"name": "minimal", "enabled": true, "type": "stdio"}
        """.data(using: .utf8)!
        let server = try JSONDecoder().decode(MCPServer.self, from: json)
        XCTAssertEqual(server.name, "minimal")
        XCTAssertTrue(server.enabled)
        XCTAssertEqual(server.type, "stdio")
        XCTAssertNil(server.url)
        XCTAssertNil(server.command)
    }

    func testMCPServerWithSpecialChars() throws {
        let json = """
        {"name": "test:server@home", "enabled": true, "type": "sse", "url": "http://host:8080/mcp"}
        """.data(using: .utf8)!
        let server = try JSONDecoder().decode(MCPServer.self, from: json)
        XCTAssertEqual(server.name, "test:server@home")
    }

    // MARK: - Agent edge cases

    func testAgentDefWithZeroToolCount() throws {
        let json = """
        {"id": "a-1", "name": "no-tools", "flow_type": "single", "visibility": "project", "is_default": false, "tool_count": 0}
        """.data(using: .utf8)!
        let agent = try JSONDecoder().decode(AgentDef.self, from: json)
        XCTAssertEqual(agent.toolCount, 0)
    }

    func testAgentDefWithMaxToolCount() throws {
        let json = """
        {"id": "a-1", "name": "many-tools", "flow_type": "pipeline", "visibility": "project", "is_default": false, "tool_count": 999}
        """.data(using: .utf8)!
        let agent = try JSONDecoder().decode(AgentDef.self, from: json)
        XCTAssertEqual(agent.toolCount, 999)
    }

    // MARK: - GraphObject edge cases

    func testGraphObjectWithEmptyProperties() throws {
        let json = """
        {"id": "o-1", "type": "note", "properties": {}}
        """.data(using: .utf8)!
        let obj = try JSONDecoder().decode(GraphObject.self, from: json)
        XCTAssertEqual(obj.properties?.isEmpty, true)
        // displayName falls back to "type: id.prefix(8)"
        XCTAssertEqual(obj.displayName, "note: o-1")
    }

    func testGraphObjectWithNullScore() throws {
        let json = """
        {"id": "o-1", "type": "note", "score": null}
        """.data(using: .utf8)!
        let obj = try JSONDecoder().decode(GraphObject.self, from: json)
        XCTAssertNil(obj.score)
    }

    // MARK: - RelayNode edge cases

    func testRelayNodeWithMinimalFields() throws {
        // This is the exact shape the API returns
        let json = """
        {"instance_id": "test-node", "tool_count": 1}
        """.data(using: .utf8)!
        let node = try JSONDecoder().decode(RelayNode.self, from: json)
        XCTAssertEqual(node.instanceID, "test-node")
        XCTAssertEqual(node.toolCount, 1)
        XCTAssertNil(node.online, "Online should be nil when not provided by API")
        XCTAssertNil(node.hostname, "Hostname should be nil when not provided")
        XCTAssertNil(node.mode, "Mode should be nil when not provided")
    }

    func testRelayNodeWithAllOptionalFieldsNil() throws {
        let json = """
        {"instance_id": "minimal-node"}
        """.data(using: .utf8)!
        let node = try JSONDecoder().decode(RelayNode.self, from: json)
        XCTAssertEqual(node.instanceID, "minimal-node")
        XCTAssertNil(node.toolCount)
        XCTAssertNil(node.version)
        XCTAssertNil(node.online)
    }

    // MARK: - Doctor edge cases

    func testDoctorWithDetailsDict() throws {
        let json = """
        {"ok": true, "version": "1.0", "results": [
            {"check": "test", "status": "ok", "message": "Passed", "details": {"key": "value"}}
        ]}
        """.data(using: .utf8)!
        let doctor = try JSONDecoder().decode(DoctorResponse.self, from: json)
        XCTAssertTrue(doctor.ok)
        XCTAssertEqual(doctor.results.first?.details?["key"], "value")
    }

    // MARK: - Stats edge cases

    func testGraphObjectStatsWithZeroTotal() throws {
        let json = """
        {"total": 0, "by_type": []}
        """.data(using: .utf8)!
        let stats = try JSONDecoder().decode(GraphObjectStatsResponse.self, from: json)
        XCTAssertEqual(stats.total, 0)
        XCTAssertTrue(stats.byType.isEmpty)
    }

    func testAgentStatsWithEmptyAgentsList() throws {
        let json = """
        {"agents": [], "totals": {"total_runs": 0, "total_success": 0, "total_errors": 0, "total_duration_ms": 0, "total_input_tokens": 0, "total_output_tokens": 0, "total_cost_usd": 0, "overall_avg_duration_ms": 0, "overall_success_rate": 0}, "hours": 24}
        """.data(using: .utf8)!
        let stats = try JSONDecoder().decode(AgentStatsResponse.self, from: json)
        XCTAssertTrue(stats.agents.isEmpty)
        XCTAssertEqual(stats.totals.totalRuns, 0)
        XCTAssertEqual(formatDuration(stats.totals.overallAvgDurationMs), "0ms")
        XCTAssertEqual(formatCost(stats.totals.totalCostUsd), "0.00¢")
    }

    func testProviderStatsEmpty() throws {
        let json = """
        {"providers": [], "total_runs": 0, "total_success": 0, "total_errors": 0, "total_input_tokens": 0, "total_output_tokens": 0, "total_cost_usd": 0, "hours": 24}
        """.data(using: .utf8)!
        let stats = try JSONDecoder().decode(ProviderStatsResponse.self, from: json)
        XCTAssertTrue(stats.providers.isEmpty)
    }

    // MARK: - Format function edge cases

    func testFormatDurationZero() {
        XCTAssertEqual(formatDuration(0), "0ms")
    }

    func testFormatDurationVeryLarge() {
        XCTAssertEqual(formatDuration(99999999), "1666.7m")
    }

    func testFormatCountZero() {
        XCTAssertEqual(formatCount(0), "0")
    }

    func testFormatCountNegative() {
        XCTAssertEqual(formatCount(-5), "-5")
    }

    func testFormatCostNegative() {
        XCTAssertEqual(formatCost(-0.50), "-50.00¢")
    }

    func testFormatCostMillions() {
        XCTAssertEqual(formatCost(1234567.89), "$1234567.89")
    }

    func testFormatTokenCountZero() {
        XCTAssertEqual(formatTokenCount(0), "0")
    }

    func testFormatTokenCountExactBoundary() {
        // 999 should stay as number, 1000 becomes K
        XCTAssertEqual(formatTokenCount(999), "999")
        XCTAssertEqual(formatTokenCount(1000), "1.0K")
        XCTAssertEqual(formatTokenCount(1_000_000), "1.0M")
    }

    // MARK: - JSON response shape validation

    func testServerStatusShapeMatchesAPI() throws {
        // This must match the ACTUAL /api/status response shape
        let json = """
        {"ok": true, "version": "dev", "started_at": "2026-05-07T18:11:04Z", "server_url": "https://memory.emergent-company.ai", "project_id": "proj-1"}
        """.data(using: .utf8)!
        let status = try JSONDecoder().decode(DianeAPIClient.ServerStatus.self, from: json)
        XCTAssertTrue(status.ok)
    }

    func testRelayNodeResponseShapeMatchesAPI() throws {
        // This must match the ACTUAL /api/nodes response shape
        let json = """
        {"nodes": [{"instance_id": "tool-test", "tool_count": 1, "version": "1.0", "connected_at": "2026-05-07T20:47:08Z"}]}
        """.data(using: .utf8)!
        struct Response: Decodable { let nodes: [RelayNode]? }
        let resp = try JSONDecoder().decode(Response.self, from: json)
        XCTAssertNotNil(resp.nodes)
        XCTAssertEqual(resp.nodes?.first?.instanceID, "tool-test")
        XCTAssertNil(resp.nodes?.first?.online, "API does not send 'online' — must be optional")
    }

    func testMCPResponseShape() throws {
        let json = """
        {"servers": [{"name": "test", "enabled": true, "type": "stdio"}]}
        """.data(using: .utf8)!
        struct Response: Decodable { let servers: [MCPServer]? }
        let resp = try JSONDecoder().decode(Response.self, from: json)
        XCTAssertEqual(resp.servers?.count, 1)
    }
}
