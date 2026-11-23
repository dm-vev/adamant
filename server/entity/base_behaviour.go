package entity

// BaseBehaviour provides shared runtime state for Ent behaviours.
type baseBehaviour struct {
	portalTravel *PortalTravelComputer
}

// NewBaseBehaviour returns a BaseBehaviour initialised with the default Ent runtime behaviour.
func newBaseBehaviour() baseBehaviour {
	return baseBehaviour{portalTravel: NewPortalTravelComputer()}
}

// PortalTravelComputer returns the portal travel state for a behaviour.
func (b *baseBehaviour) PortalTravelComputer() *PortalTravelComputer {
	if b.portalTravel == nil {
		b.portalTravel = NewPortalTravelComputer()
	}
	return b.portalTravel
}
