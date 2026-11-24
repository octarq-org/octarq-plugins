package mail

import "github.com/octarq-org/octarq/server/plugin"

const ServiceStorageProvider = "storage.provider"

const (
	metricRawBytes = "mail.raw_bytes"
	metricMailIn   = "mailIn"
	metricMailOut  = "mailOut"
)

var _ plugin.StorageProvider = (*DBStorageProvider)(nil)
