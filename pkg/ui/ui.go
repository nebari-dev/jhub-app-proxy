package ui

import _ "embed"

//go:embed logs.html
var LogsHTML string

//go:embed logs.css
var LogsCSS string

//go:embed logs.js
var LogsJS string

//go:embed UniMannheim-Symbol.png
var LogoPNG []byte
