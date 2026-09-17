package isolation

import model "retrom/internal/model/isolation"

var ErrCredential = model.ErrCredential

type (
	Repository             = model.Repository
	Access                 = model.Access
	RuntimeSession         = model.RuntimeSession
	Bootstrap              = model.Bootstrap
	Capability             = model.Capability
	TicketQuery            = model.TicketQuery
	CredentialQuery        = model.CredentialQuery
	CapabilityWrite        = model.CapabilityWrite
	ConsumeAndIssueCommand = model.ConsumeAndIssueCommand
	ConsumeAndIssueResult  = model.ConsumeAndIssueResult
)

// ActiveSession delegates to the model's pure session check.
var ActiveSession = model.ActiveSession
