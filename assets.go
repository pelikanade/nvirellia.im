// Package nvirellia embeds the site's templates and static assets.
package nvirellia

import "embed"

// Assets holds templates/ and static/.
//
//go:embed templates static
var Assets embed.FS
