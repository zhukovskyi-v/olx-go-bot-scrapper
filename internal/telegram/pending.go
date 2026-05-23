package telegram

type pendingKind int

const (
	pendingNone pendingKind = iota
	pendingAddURL
	pendingFilterPrice
	pendingFilterInclude
	pendingFilterExclude
)

type pendingInput struct {
	kind    pendingKind
	localID int
}

func (b *Bot) setPending(userID int64, p pendingInput) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	if p.kind == pendingNone {
		delete(b.pending, userID)
		return
	}
	b.pending[userID] = p
}

func (b *Bot) takePending(userID int64) pendingInput {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	p := b.pending[userID]
	delete(b.pending, userID)
	return p
}

func (b *Bot) peekPending(userID int64) pendingInput {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	return b.pending[userID]
}
