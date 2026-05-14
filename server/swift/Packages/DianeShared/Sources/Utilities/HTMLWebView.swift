import SwiftUI
#if os(iOS)
import WebKit
#elseif os(macOS)
import WebKit
#endif

// MARK: - HTML Web View (Platform-Agnostic Wrapper)

/// A SwiftUI wrapper around WKWebView that renders HTML content.
/// Supports auto-sizing via JavaScript height measurement.
#if os(iOS)
public struct HTMLWebView: UIViewRepresentable {
    public let htmlContent: String
    @Binding public var dynamicHeight: CGFloat

    public init(htmlContent: String, dynamicHeight: Binding<CGFloat>) {
        self.htmlContent = htmlContent
        self._dynamicHeight = dynamicHeight
    }

    public func makeCoordinator() -> Coordinator {
        Coordinator(self)
    }

    public func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.preferences.javaScriptEnabled = false
        config.suppressesIncrementalRendering = true
        config.allowsInlineMediaPlayback = false

        let webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = context.coordinator
        webView.scrollView.isScrollEnabled = false
        webView.isOpaque = false
        webView.backgroundColor = .clear
        webView.scrollView.backgroundColor = .clear
        webView.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        let darkMode = UITraitCollection.current.userInterfaceStyle == .dark
        loadHTML(webView: webView, darkMode: darkMode)
        return webView
    }

    public func updateUIView(_ webView: WKWebView, context: Context) {
        let darkMode = UITraitCollection.current.userInterfaceStyle == .dark
        loadHTML(webView: webView, darkMode: darkMode)
    }

    private func loadHTML(webView: WKWebView, darkMode: Bool) {
        let wrapped = htmlContent.htmlWrappedForWebView(darkMode: darkMode)
        webView.loadHTMLString(wrapped, baseURL: nil)
    }

    public class Coordinator: NSObject, WKNavigationDelegate {
        var parent: HTMLWebView

        init(_ parent: HTMLWebView) {
            self.parent = parent
        }

        public func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            webView.evaluateJavaScript("document.body.scrollHeight") { [weak self] height, _ in
                guard let self = self, let h = height as? CGFloat, h > 10 else { return }
                DispatchQueue.main.async {
                    self.parent.dynamicHeight = h
                }
            }
        }
    }
}
#elseif os(macOS)
public struct HTMLWebView: NSViewRepresentable {
    public let htmlContent: String
    @Binding public var dynamicHeight: CGFloat

    public init(htmlContent: String, dynamicHeight: Binding<CGFloat>) {
        self.htmlContent = htmlContent
        self._dynamicHeight = dynamicHeight
    }

    public func makeCoordinator() -> Coordinator {
        Coordinator(self)
    }

    public func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        if #available(macOS 11.0, *) {
            let preferences = WKWebpagePreferences()
            preferences.allowsContentJavaScript = false
            config.defaultWebpagePreferences = preferences
        } else {
            config.preferences.javaScriptEnabled = false
        }
        config.suppressesIncrementalRendering = true

        let webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = context.coordinator
        webView.setValue(false, forKey: "drawsBackground")
        #if swift(>=5.9)
        if #available(macOS 14.0, *) {
            webView.layer?.backgroundColor = NSColor.clear.cgColor
        }
        #endif
        webView.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        let darkMode = NSApp.effectiveAppearance.name == .darkAqua
        loadHTML(webView: webView, darkMode: darkMode)
        return webView
    }

    public func updateNSView(_ webView: WKWebView, context: Context) {
        let darkMode = NSApp.effectiveAppearance.name == .darkAqua
        loadHTML(webView: webView, darkMode: darkMode)
    }

    private func loadHTML(webView: WKWebView, darkMode: Bool) {
        let wrapped = htmlContent.htmlWrappedForWebView(darkMode: darkMode)
        webView.loadHTMLString(wrapped, baseURL: nil)
    }

    public class Coordinator: NSObject, WKNavigationDelegate {
        var parent: HTMLWebView

        init(_ parent: HTMLWebView) {
            self.parent = parent
        }

        public func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            webView.evaluateJavaScript("document.body.scrollHeight") { [weak self] height, _ in
                guard let self = self, let h = height as? CGFloat, h > 10 else { return }
                DispatchQueue.main.async {
                    self.parent.dynamicHeight = h
                }
            }
        }
    }
}
#endif
