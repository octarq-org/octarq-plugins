package mail

import "github.com/octarq-org/octarq/server/plugin"

// Compile-time checks for storage provider capability.
var _ plugin.StorageProvider = (*DBStorageProvider)(nil)
