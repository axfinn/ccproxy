package web

import "embed"

// DistFS contains the built frontend files
//
//go:embed dist/*
var DistFS embed.FS
