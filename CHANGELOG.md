# Changelog

All notable changes to this project are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- S0 skeleton: chi router with `GET /health` and a mock `POST /oauth/token`, embedded CGO-free SQLite store with the append-only `events` log and the `charges` projection, seeded deterministic random source, Makefile, and a multi-stage distroless Dockerfile.
- PowerShell task runners for Windows (`make.ps1`, `run.ps1`, `build.ps1`) and a `help` target in the Makefile.
