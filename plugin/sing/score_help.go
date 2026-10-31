package sing

import _ "embed"

// Regenerate with scripts/render-help.py. Embedding keeps help available
// without runtime fonts, a working-directory dependency, or a separate upload.
//
//go:embed assets/score-help.png
var scoreHelpImage []byte

//go:embed assets/sing-help.png
var singHelpImage []byte
