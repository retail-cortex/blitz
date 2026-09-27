// Package api is the contract between Blitz's front ends and its engine:
// the Backend a front end drives, and the values, events and errors that
// cross it. The engine's Workspace implements Backend in process and the
// client implements it over the service's API, so the CLI, the desktop
// app and the service see the same types.
//
// It depends on nothing else in Blitz except the shared config and images
// packages, and nothing here reaches into the engine.
package api
