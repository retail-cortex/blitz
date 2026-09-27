package main

// Wails's file dialogs use UTType, which newer macOS SDKs keep in its own
// framework; `wails build` adds it through CGO_LDFLAGS, Bazel through this.

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
