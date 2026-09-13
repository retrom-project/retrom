package launch

func (service *Service) isolatedRuntimeTicket(id string) (string, string, [32]byte, error) {
	ticket, err := service.sources().SignIsolation(id)
	return ticket.Origin, ticket.Ticket, ticket.Hash, err
}
