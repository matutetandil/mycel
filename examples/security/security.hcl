# Security configuration
#
# Mycel sanitizes ALL input automatically (null bytes, invalid UTF-8,
# control chars, bidi overrides, XXE, path traversal, shell injection).
# This block lets you adjust thresholds and add custom sanitizers.

security {
  # Maximum total input size in bytes (default: 1MB)
  max_input_length = 524288  # 512KB — stricter than default

  # Maximum length of a single string field (default: 64KB)
  max_field_length = 8192    # 8KB

  # Maximum nesting depth for JSON input (default: 20)
  max_field_depth = 10

  # Control characters allowed through sanitization (default: tab, newline, cr)
  allowed_control_chars = ["tab", "newline"]

  # Per-flow overrides — raise limits for specific flows
  flow "bulk_import" {
    max_input_length = 10485760  # 10MB for bulk operations
  }

  # Custom WASM sanitizer example (uncomment to use):
  # sanitizer "strip_html" {
  #   source     = "wasm"
  #   wasm       = "plugins/strip_html.wasm"
  #   entrypoint = "sanitize"
  #   apply_to   = ["flows/*"]
  #   fields     = ["name", "bio"]
  # }
}
