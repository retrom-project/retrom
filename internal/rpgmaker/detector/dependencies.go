package detector

// ExternalRTPRequirements describes declarations that need resources outside the
// uploaded project. It does not claim to prove dynamic script asset references.
func ExternalRTPRequirements(generation Generation, selfContained bool, declared []RTPDependency) []RTPDependency {
	result := make([]RTPDependency, 0, len(declared))
	switch generation {
	case RPG2000, RPG2003:
		if !selfContained {
			name := "RPG2000_RTP"
			if generation == RPG2003 {
				name = "RPG2003_RTP"
			}
			result = append(result, RTPDependency{Slot: 0, DeclaredName: name})
		}
	case RPGMV, RPGMZ:
		return result
	case RPGXP, RPGVX, RPGVXAce:
		result = append(result, declared...)
	}
	return result
}
