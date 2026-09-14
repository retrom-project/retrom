package isolation

import model "retrom/internal/model/isolation"

var ErrCredential = model.ErrCredential

type (
	Repository      = model.Repository
	Tickets         = model.Tickets
	Access          = model.Access
	RuntimeSession  = model.RuntimeSession
	Bootstrap       = model.Bootstrap
	Capability      = model.Capability
	TicketQuery     = model.TicketQuery
	CredentialQuery = model.CredentialQuery
	CapabilityWrite = model.CapabilityWrite
)
