package mail

import "github.com/octarq-org/octarq/server/plugin"

var _ plugin.StorageProvider = (*DBStorageProvider)(nil)
