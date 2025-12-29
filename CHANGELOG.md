# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **ASCII art banner** with colored terminal output
  - New `internal/banner/` package for styled console output
  - ANSI color support with automatic detection (respects NO_COLOR env var)
  - Color-coded HTTP methods (GET=green, POST=yellow, DELETE=magenta)
  - Clean startup display with service info, connectors, and flows

### Fixed
- **GET with path parameters** now correctly filters results
  - Operations like `GET /users/:id` automatically extract path params as query filters
  - `extractPathParams()` helper function added to flow registry

### Added (Phase 1)
- **`mycel start` command is now functional!**
  - Full runtime orchestration: parse config → init connectors → register flows → start HTTP server
  - Graceful shutdown with SIGINT/SIGTERM handling
- **REST connector** (`internal/connector/rest/`)
  - HTTP server with configurable port and CORS
  - Automatic route registration from flow configurations
  - JSON request/response handling
- **SQLite connector** (`internal/connector/database/sqlite/`)
  - Full CRUD operations (SELECT, INSERT, UPDATE, DELETE)
  - Pure Go driver (no CGO required) via `modernc.org/sqlite`
  - Connection pooling and health checks
- **Runtime engine** (`internal/runtime/`)
  - Configuration-driven service orchestration
  - Flow registry with automatic handler building
  - Connector lifecycle management
- Working example in `examples/basic/` with SQLite database
- `mycel validate` command to check configuration validity
- `mycel check` command to verify connector configuration

### Changed
- **BREAKING:** Updated flow block syntax for HCL compatibility
  - `from` block now uses `connector` and `operation` attributes
  - `to` block now uses `connector`, `target`, and optional `filter` attributes
  - Old syntax: `from { connector.api = "GET /users" }`
  - New syntax: `from { connector = "api", operation = "GET /users" }`

### Fixed
- Fixed `TestParseFlow` and `TestParseDirectory` parser tests
- Updated example files to use valid HCL syntax
- Fixed connector driver parsing in HCL parser

### Added (Initial)
- Initial project setup
- Project specification and design documents (CLAUDE.md)
- CLI scaffolding with cobra (start, validate, check commands)
- HCL parser for connectors, flows, types, and service blocks
- Connector interfaces (Reader, Writer, ReadWriter, Registry, Factory)
- Flow executor with pipeline pattern and stages
- Validation system with TypeValidator and built-in constraints
- Transform system with FunctionRegistry
- Custom HCL functions: `env()`, `coalesce()`

---

## Version History

_No releases yet. Development starting from Fase 1 - Core._
