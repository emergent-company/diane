import SwiftUI

/// Ask view — lists agent questions and lets the user respond.
/// Shows pending questions from agents, supporting all interaction types:
/// buttons, select, multi_select, and free-text input.
struct QuestionsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration

    @State private var questions: [AgentQuestion] = []
    @State private var selectedQuestion: AgentQuestion? = nil
    @State private var isLoading = false
    @State private var error: String?

    // Response state
    @State private var textResponse: String = ""
    @State private var selectedOptions: Set<String> = []
    @State private var isSubmitting = false
    @State private var submitError: String?
    @State private var submitSuccess: String?

    // Filter
    @State private var showOnlyPending = true

    private var projectID: String { serverConfig.projectID }

    private static nonisolated(unsafe) let isoFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    private static nonisolated(unsafe) let isoFormatterNoFractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    var body: some View {
        SplitListDetailView(
            emptyTitle: "No Questions",
            emptyIcon: "questionmark.bubble",
            emptyDescription: showOnlyPending
                ? "No pending questions from agents."
                : "No questions found in this project.",
            listMinWidth: 280,
            listContent: { questionsList },
            detailContent: {
                if let q = selectedQuestion {
                    questionDetailPanel(q: q)
                } else {
                    AskEmptyStateView(
                        title: "Select a Question",
                        icon: "questionmark.bubble",
                        description: "Select a pending question to respond to it."
                    )
                }
            }
        )
        .navigationTitle("Ask")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Picker("Filter", selection: $showOnlyPending) {
                    Text("Pending").tag(true)
                    Text("All").tag(false)
                }
                .pickerStyle(.segmented)
                .fixedSize()
            }
            ToolbarItem(placement: .automatic) {
                Button(action: { Task { await load() } }) {
                    Image(systemName: "arrow.clockwise")
                }
                .disabled(isLoading)
            }
        }
        .task { await load() }
        .onChange(of: showOnlyPending) { _, _ in Task { await load() } }
    }

    // MARK: - Load

    private func load() async {
        guard !projectID.isEmpty else { return }
        isLoading = true
        error = nil
        do {
            let status: AgentQuestionStatus? = showOnlyPending ? .pending : nil
            questions = try await apiClient.fetchAgentQuestions(
                projectID: projectID,
                status: status
            )
            // Maintain selection if still valid
            if let sel = selectedQuestion {
                selectedQuestion = questions.first { $0.id == sel.id }
            }
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }

    // MARK: - List

    private var questionsList: some View {
        List(selection: $selectedQuestion) {
            if isLoading && questions.isEmpty {
                HStack {
                    Spacer()
                    ProgressView()
                    Spacer()
                }
                .padding()
            } else if let err = error {
                HStack {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(.orange)
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding()
            } else if questions.isEmpty {
                Text(showOnlyPending ? "No pending questions" : "No questions")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding()
            }

            ForEach(questions) { question in
                QuestionRowView(question: question)
                    .tag(question)
            }
        }
        .listStyle(.bordered(alternatesRowBackgrounds: true))
    }

    // MARK: - Detail Panel

    @ViewBuilder
    private func questionDetailPanel(q: AgentQuestion) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                // Header
                HStack {
                    Image(systemName: "brain.head.profile")
                        .font(.title3)
                        .foregroundStyle(Color.accentColor)
                    Text("Agent Question")
                        .font(.headline)
                    Spacer()
                    AskStatusBadgeView(status: q.status.displayLabel, color: statusColor(q.status))
                }

                // Question text
                VStack(alignment: .leading, spacing: 8) {
                    Text("Question")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Text(q.question)
                        .font(.body)
                        .textSelection(.enabled)
                }

                // Metadata
                HStack(spacing: 16) {
                    Label("Run: \(q.runId.prefix(12))…", systemImage: "arrow.triangle.branch")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    if let at = formattedDate(q.createdAt) {
                        Label(at, systemImage: "clock")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                Divider()

                // Response area
                if q.status == .pending {
                    responseSection(q)
                } else if let resp = q.response {
                    answeredSection(q, response: resp)
                } else {
                    Text("This question has been \(q.status.rawValue).")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .padding()
        }
    }

    // MARK: - Response Input

    @ViewBuilder
    private func responseSection(_ q: AgentQuestion) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Your Response")
                .font(.headline)

            switch q.interactionType {
            case .buttons:
                buttonsResponse(q)
            case .select:
                selectResponse(q)
            case .multiSelect:
                multiSelectResponse(q)
            case .text:
                textResponseField(q)
            }

            if let err = submitError {
                HStack {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.red)
                }
            }

            if let success = submitSuccess {
                HStack {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                    Text(success)
                        .font(.caption)
                        .foregroundStyle(.green)
                }
            }

            Button(action: { Task { await submitResponse(q) } }) {
                if isSubmitting {
                    ProgressView()
                        .controlSize(.small)
                } else {
                    Text("Submit Response")
                }
            }
            .buttonStyle(.borderedProminent)
            .disabled(isSubmitting || responseIsEmpty(q))
        }
    }

    private var hasOptions: Bool {
        guard let opts = selectedQuestion?.options, !opts.isEmpty else { return false }
        return true
    }

    private func responseIsEmpty(_ q: AgentQuestion) -> Bool {
        switch q.interactionType {
        case .buttons, .select:
            return selectedOptions.isEmpty
        case .multiSelect:
            return selectedOptions.isEmpty
        case .text:
            return textResponse.trimmingCharacters(in: .whitespaces).isEmpty
        }
    }

    // MARK: - Interaction Types

    private func buttonsResponse(_ q: AgentQuestion) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(q.options ?? [], id: \.value) { option in
                let isSelected = selectedOptions.contains(option.value)
                Button(action: { selectedOptions = [option.value] }) {
                    HStack {
                        Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(isSelected ? Color.accentColor : Color.secondary)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(option.label)
                                .fontWeight(isSelected ? .semibold : .regular)
                        }
                        if let desc = option.description {
                            Text(desc)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                    }
                    .padding(10)
                    .background(optionBackground(isSelected: isSelected))
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(isSelected ? Color.accentColor : Color(nsColor: NSColor.separatorColor), lineWidth: 1)
                    )
                }
                .buttonStyle(.plain)
            }
        }
    }

    private func optionBackground(isSelected: Bool) -> some View {
        if isSelected {
            return Color.accentColor.opacity(0.1)
        } else {
            return Color(nsColor: NSColor.controlBackgroundColor)
        }
    }

    private func selectResponse(_ q: AgentQuestion) -> some View {
        Picker("Select option", selection: $selectedOptions) {
            Text("— Select —").tag(Set<String>())
            ForEach(q.options ?? [], id: \.value) { option in
                Text(option.label).tag(Set([option.value]))
            }
        }
        .pickerStyle(.menu)
        .labelsHidden()
    }

    private func multiSelectResponse(_ q: AgentQuestion) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(q.options ?? [], id: \.value) { option in
                Toggle(isOn: Binding(
                    get: { selectedOptions.contains(option.value) },
                    set: { on in
                        if on { selectedOptions.insert(option.value) }
                        else { selectedOptions.remove(option.value) }
                    }
                )) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(option.label)
                        if let desc = option.description {
                            Text(desc)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
    }

    private func textResponseField(_ q: AgentQuestion) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            TextEditor(text: $textResponse)
                .font(.body)
                .frame(minHeight: 80, maxHeight: 200)
                .overlay(
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(Color(nsColor: NSColor.separatorColor), lineWidth: 1)
                )
                .overlay(alignment: .topLeading) {
                    if textResponse.isEmpty, let ph = q.placeholder {
                        Text(ph)
                            .font(.body)
                            .foregroundStyle(.tertiary)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 8)
                            .allowsHitTesting(false)
                    }
                }

            if let maxLen = q.maxLength, maxLen > 0 {
                HStack {
                    Spacer()
                    Text("\(textResponse.count)/\(maxLen)")
                        .font(.caption)
                        .foregroundStyle(textResponse.count > maxLen ? .red : .secondary)
                }
            }
        }
    }

    // MARK: - Answered section

    private func answeredSection(_ q: AgentQuestion, response: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Your Response")
                .font(.headline)

            Text(response)
                .font(.body)
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(nsColor: NSColor.controlBackgroundColor))
                .cornerRadius(8)

            if let at = q.respondedAt, let date = formattedDate(at) {
                Text("Answered \(date)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Submit

    private func submitResponse(_ q: AgentQuestion) async {
        guard !projectID.isEmpty else { return }
        isSubmitting = true
        submitError = nil
        submitSuccess = nil

        let responseText: String
        switch q.interactionType {
        case .buttons, .select:
            responseText = selectedOptions.first ?? ""
        case .multiSelect:
            responseText = selectedOptions.sorted().joined(separator: ", ")
        case .text:
            responseText = textResponse.trimmingCharacters(in: .whitespacesAndNewlines)
        }

        guard !responseText.isEmpty else {
            submitError = "Please provide a response."
            isSubmitting = false
            return
        }

        do {
            try await apiClient.respondToAgentQuestion(
                projectID: projectID,
                questionID: q.id,
                response: responseText
            )
            submitSuccess = "Response submitted. Agent is resuming..."
            // Reset input
            textResponse = ""
            selectedOptions = []
            // Refresh after brief delay for server processing
            try? await Task.sleep(nanoseconds: 1_000_000_000)
            await load()
        } catch {
            submitError = error.localizedDescription
        }
        isSubmitting = false
    }

    // MARK: - Helpers

    private func statusColor(_ status: AgentQuestionStatus) -> Color {
        switch status {
        case .pending:   return .orange
        case .answered:  return .green
        case .expired:   return .gray
        case .cancelled: return .secondary
        }
    }

    private func formattedDate(_ iso: String) -> String? {
        guard let date = Self.isoFormatter.date(from: iso) ?? Self.isoFormatterNoFractional.date(from: iso) else { return nil }

        let elapsed = Date().timeIntervalSince(date)
        if elapsed < 3600 { // < 1 hour
            let rel = RelativeDateTimeFormatter()
            rel.unitsStyle = .full
            return rel.localizedString(for: date, relativeTo: Date())
        } else if elapsed < 86400 * 7 { // < 1 week
            let rel = RelativeDateTimeFormatter()
            rel.unitsStyle = .full
            return rel.localizedString(for: date, relativeTo: Date())
        } else {
            // Absolute date for older items
            let df = DateFormatter()
            df.dateFormat = "MMM d"
            return df.string(from: date)
        }
    }
}

// MARK: - Row View

private struct QuestionRowView: View {
    let question: AgentQuestion

    private static nonisolated(unsafe) let isoFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    private static nonisolated(unsafe) let isoFormatterNoFractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    var body: some View {
        HStack(spacing: 10) {
            // Status indicator
            Circle()
                .fill(statusDotColor)
                .frame(width: 10, height: 10)

            VStack(alignment: .leading, spacing: 3) {
                Text(question.question)
                    .font(.body)
                    .lineLimit(2)

                HStack(spacing: 8) {
                    Text(question.status.displayLabel)
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    if let at = formattedDate(iso: question.createdAt) {
                        Text(at)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    if question.interactionType == .text {
                        Image(systemName: "pencil")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    } else if let opts = question.options {
                        Text("\(opts.count) option\(opts.count == 1 ? "" : "s")")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }
            }
        }
        .padding(.vertical, 2)
    }

    private var statusDotColor: Color {
        switch question.status {
        case .pending:   return .orange
        case .answered:  return .green
        case .expired:   return .gray
        case .cancelled: return .secondary
        }
    }

    private func formattedDate(iso: String) -> String? {
        guard let date = Self.isoFormatter.date(from: iso) ?? Self.isoFormatterNoFractional.date(from: iso) else { return nil }

        let elapsed = Date().timeIntervalSince(date)
        if elapsed < 86400 * 7 { // < 1 week
            let rel = RelativeDateTimeFormatter()
            rel.unitsStyle = .full
            return rel.localizedString(for: date, relativeTo: Date())
        } else {
            // Absolute date for older items
            let df = DateFormatter()
            df.dateFormat = "MMM d"
            return df.string(from: date)
        }
    }
}

// MARK: - Empty State

private struct AskEmptyStateView: View {
    let title: String
    let icon: String
    let description: String

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: icon)
                .font(.system(size: 40))
                .foregroundStyle(.secondary)
            Text(title)
                .font(.title3)
                .fontWeight(.semibold)
            Text(description)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: - Status Badge (filename-safe local type)

private struct AskStatusBadgeView: View {
    let status: String
    let color: Color

    var body: some View {
        Text(status)
            .font(.caption)
            .fontWeight(.medium)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(color.opacity(0.15))
            .foregroundStyle(color)
            .clipShape(Capsule())
    }
}
