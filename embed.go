package main

import (
	"embed"
)

//go:embed web/templates/*
var templatesFS embed.FS

//go:embed web/static/*
var staticFS embed.FS
