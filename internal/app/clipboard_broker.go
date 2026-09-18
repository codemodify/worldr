package app

// clipboardBroker shares one selection between the desktop, legacy clients,
// and native applications. Tokens identify the original endpoint and offer;
// contents are transferred only when a destination requests a paste.
type clipboardBroker struct {
	endpoints []clipboardEndpoint
	revisions []uint64
	pending   []*clipboardOffer
	serial    uint64
	owner     int
	offerID   uint64
	token     uint64
}

func newClipboardBroker(endpoints ...clipboardEndpoint) *clipboardBroker {
	return &clipboardBroker{endpoints: endpoints, revisions: make([]uint64, len(endpoints)), pending: make([]*clipboardOffer, len(endpoints)), owner: -1}
}

func (b *clipboardBroker) sync() {
	winner := -1
	var selection clipboardOffer
	for i, endpoint := range b.endpoints {
		offer := endpoint.offer()
		if offer.revision != b.revisions[i] && offer.available && offer.external == 0 {
			// Providers later in the list win simultaneous copies; native and
			// hosted apps follow the desktop so a fresh local copy is retained.
			winner, selection = i, offer
		}
		b.revisions[i] = offer.revision
	}
	if winner >= 0 {
		b.owner, b.offerID = winner, selection.id
		b.token = 0
		if selection.id != 0 {
			b.serial++
			b.token = b.serial
		}
		for i := range b.endpoints {
			b.pending[i] = nil
			if i != winner {
				b.pending[i] = &clipboardOffer{id: b.token, mimes: append([]string(nil), selection.mimes...)}
			}
		}
	}
	for i, endpoint := range b.endpoints {
		if offer := b.pending[i]; offer != nil {
			if err := endpoint.publish(offer.id, offer.mimes); err == nil {
				b.pending[i] = nil
				// Ignore synchronous echo metadata, including a cleared source.
				b.revisions[i] = endpoint.offer().revision
			}
		}
	}
	for _, endpoint := range b.endpoints {
		for _, request := range endpoint.requests() {
			if b.owner >= 0 && b.token != 0 && request.source == b.token {
				_ = b.endpoints[b.owner].receive(b.offerID, request.mime, request.fd)
			}
			closeClipboardFD(request.fd)
		}
	}
}
