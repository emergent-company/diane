import Foundation

@MainActor
enum AppleScriptRunner {
    
    @discardableResult
    static func run(_ script: String) async throws -> String {
        return try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                let process = Process()
                process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
                process.arguments = ["-e", script]
                
                let outputPipe = Pipe()
                let errorPipe = Pipe()
                process.standardOutput = outputPipe
                process.standardError = errorPipe
                
                do {
                    try process.run()
                    process.waitUntilExit()
                    
                    let outputData = outputPipe.fileHandleForReading.readDataToEndOfFile()
                    let errorData = errorPipe.fileHandleForReading.readDataToEndOfFile()
                    let output = String(data: outputData, encoding: .utf8) ?? ""
                    let errorOutput = String(data: errorData, encoding: .utf8) ?? ""
                    
                    if process.terminationStatus == 0 {
                        continuation.resume(returning: output.trimmingCharacters(in: .whitespacesAndNewlines))
                    } else {
                        let errorMsg = errorOutput.isEmpty ? "AppleScript failed with exit code \(process.terminationStatus)" : errorOutput
                        continuation.resume(throwing: NSError(domain: "AppleScript", code: Int(process.terminationStatus), userInfo: [NSLocalizedDescriptionKey: errorMsg]))
                    }
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }
}
