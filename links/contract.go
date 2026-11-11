package links

import "github.com/octarq-org/octarq/server/plugin"

// Assert that Plugin implements the cross-plugin short-link creation SPI.
var _ plugin.LinkCreator = (*Plugin)(nil)
