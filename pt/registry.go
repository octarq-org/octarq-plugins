package pt

import (
	"github.com/octarq-org/octarq-plugins/pt/adapter"
	"github.com/octarq-org/octarq-plugins/pt/adapters/hddolby"
	"github.com/octarq-org/octarq-plugins/pt/adapters/mteam"
	"github.com/octarq-org/octarq-plugins/pt/adapters/nexusphp"
	"github.com/octarq-org/octarq-plugins/pt/adapters/rousi"
)

// buildRegistry compiles in every supported dialect.
//
// Domain hints only preselect the schema in the add form. The stored schema is
// always the one the user confirmed, because the NexusPHP family has branches
// that no probe can tell apart reliably.
func buildRegistry() *adapter.Registry {
	r := adapter.NewRegistry()
	r.MustRegister(nexusphp.New())
	r.MustRegister(mteam.New(), "m-team.cc", "m-team.io", "kp.m-team.cc")
	r.MustRegister(hddolby.New(), "hddolby.com")
	r.MustRegister(rousi.New(), "rousi.pro")
	return r
}
